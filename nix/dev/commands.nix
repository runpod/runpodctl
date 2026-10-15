#
# nix/dev/commands.nix — runpodctl
#
# Single source of truth for every command a developer runs by name: the
# dev-shell helpers *and* `nix run .#<name>` apps are both generated from this
# one table (./shell.nix puts them on PATH and renders the banner; nix/apps
# turns each into an app), so the two can't disagree.
#
# Each entry is a writeShellApplication, so the script is shellchecked at
# build time and runs under `set -o errexit -o nounset -o pipefail`. They run
# in the working tree (not the sandbox), so each starts by checking it's at
# the repo root.
#
# Entry shape: { group; description; runtimeInputs ? [ ]; text; }.
#
{
  pkgs,
  lib,
  versions,
  images,
  staticAnalysis,
  tests,
}:
let
  # Shared prelude: run from the repo root, with the pinned Go and no
  # toolchain downloads.
  prelude = ''
    if [ ! -f flake.nix ] || [ ! -f go.mod ]; then
      echo "run this from the runpodctl repo root (where flake.nix is)" >&2
      exit 2
    fi
    export GOTOOLCHAIN=local
  '';

  goTools = [ versions.go ];

  golangciTier = config: description: {
    group = "lint";
    inherit description;
    runtimeInputs = goTools ++ [ versions.golangci-lint ];
    text = ''golangci-lint run --config ${config} "$@" ./...'';
  };

  inherit (staticAnalysis) staticreport toolArgs;
  measure = staticAnalysis.static-analysis-measure;

  table = {
    # ---- correctness -------------------------------------------------------
    check-vet = {
      group = "check";
      description = "go vet, with and without the e2e build tag";
      runtimeInputs = goTools;
      text = ''
        go vet ./...
        go vet -tags e2e ./...
      '';
    };
    check-fmt = {
      group = "check";
      description = "list files gofmt would change";
      runtimeInputs = goTools;
      text = ''
        unformatted="$(gofmt -l .)"
        if [ -n "$unformatted" ]; then
          echo "$unformatted"
          exit 1
        fi
      '';
    };
    # Not a flake check: tidy needs the test-only dependencies of every
    # dependency, which the build's module cache (correctly) doesn't hold.
    check-tidy = {
      group = "check";
      description = "go mod tidy -diff (needs network)";
      runtimeInputs = goTools;
      text = "go mod tidy -diff";
    };
    check-test = {
      group = "check";
      description = "unit tests with coverage (real ssh/rsync on PATH)";
      runtimeInputs = goTools ++ [
        pkgs.openssh
        pkgs.rsync
      ];
      text = ''go test -count=1 -cover "$@" ./...'';
    };
    check-race = {
      group = "check";
      description = "unit tests under the race detector (cmd/transfer races in croc today)";
      runtimeInputs = goTools ++ [
        pkgs.openssh
        pkgs.rsync
        pkgs.stdenv.cc
      ];
      text = ''CGO_ENABLED=1 go test -race -count=1 "$@" ./...'';
    };
    check-all = {
      group = "check";
      description = "every hermetic check: nix flake check -L";
      text = ''nix flake check -L "$@"'';
    };

    # ---- static analysis ---------------------------------------------------
    lint-quick = golangciTier ".golangci-quick.yml" "golangci-lint tier 0 (correctness)";
    lint = golangciTier ".golangci.yml" "golangci-lint tier 1 (standard)";
    lint-comprehensive = golangciTier ".golangci-comprehensive.yml" "golangci-lint tier 2 (comprehensive)";
    lint-fix = {
      group = "lint";
      description = "apply golangci-lint tier 1 autofixes to the working tree";
      runtimeInputs = goTools ++ [ versions.golangci-lint ];
      text = ''golangci-lint run --config .golangci.yml --fix "$@" ./...'';
    };
    check-staticcheck = {
      group = "lint";
      description = "staticcheck -checks all";
      runtimeInputs = goTools ++ [ versions.staticcheck ];
      text = ''staticcheck -checks all -tags e2e "$@" ./...'';
    };
    check-gosec = {
      group = "lint";
      description = "gosec security scan";
      runtimeInputs = goTools ++ [ versions.gosec ];
      text = ''gosec -tags e2e "$@" ./...'';
    };
    govulncheck = {
      group = "lint";
      description = "known vulnerabilities reachable from the code (needs network)";
      runtimeInputs = goTools ++ [ versions.govulncheck ];
      text = ''govulncheck "$@" ./...'';
    };
    update-lint-baseline = {
      group = "lint";
      description = "rewrite nix/checks/lint-baseline.txt after fixing findings";
      runtimeInputs = [
        staticreport
        pkgs.git
      ];
      text = ''
        staticreport baseline -update -baseline nix/checks/lint-baseline.txt \
          -root "$(cat ${measure}/root.txt)" ${toolArgs measure}
        git diff --stat -- nix/checks/lint-baseline.txt
      '';
    };
    static-analysis-report = {
      group = "lint";
      description = "print the hermetic static analysis report (no govulncheck)";
      text = "cat ${staticAnalysis.static-analysis-report}/report.md";
    };
    static-analysis-report-full = {
      group = "lint";
      description = "the static analysis report plus govulncheck -> ./static-analysis-report/";
      runtimeInputs = goTools ++ [
        staticreport
        versions.govulncheck
      ];
      text = ''
        dir="''${1:-static-analysis-report}"
        rm -rf "$dir"
        mkdir -p "$dir"
        cp -r --no-preserve=mode ${measure}/raw "$dir/raw"
        rc=0
        govulncheck -format json ./... > "$dir/govulncheck.json" || rc=$?
        if [ "$rc" -ne 0 ]; then
          echo "govulncheck failed (exit $rc); see $dir/govulncheck.json" >&2
          exit "$rc"
        fi
        staticreport report -baseline nix/checks/lint-baseline.txt \
          -root "$(cat ${measure}/root.txt)" ${toolArgs measure} \
          -meta-file ${measure}/meta/versions.txt -vuln "$dir/govulncheck.json" \
          -md "$dir/report.md" -json "$dir/report.json"
        echo "wrote $dir/report.md"
      '';
    };

    # ---- fuzzing and benchmarks --------------------------------------------
    fuzz = {
      group = "fuzz";
      description = "long fuzzing: [--fuzztime ${versions.fuzzTime.local} | --budget ci|nightly] [--pkg SUBSTR] [--target NAME]";
      runtimeInputs = goTools ++ [
        pkgs.openssh
        pkgs.rsync
      ];
      text = ''
        fuzztime="${versions.fuzzTime.local}" pkg_filter="" target_filter=""
        while [ "$#" -gt 0 ]; do
          case "$1" in
            --fuzztime) fuzztime="$2"; shift 2 ;;
            --budget)
              case "$2" in
                ${lib.concatStrings (
                  lib.mapAttrsToList (n: v: "${n}) fuzztime=\"${v}\" ;; ") versions.fuzzTime
                )}
                *) echo "unknown budget: $2 (${lib.concatStringsSep ", " (lib.attrNames versions.fuzzTime)})" >&2; exit 2 ;;
              esac
              shift 2 ;;
            --pkg) pkg_filter="$2"; shift 2 ;;
            --target) target_filter="$2"; shift 2 ;;
            *) echo "unknown argument: $1" >&2; exit 2 ;;
          esac
        done
        procs="$(( $(nproc) / 2 ))"
        if [ "$procs" -lt 1 ]; then procs=1; fi

        ran=0 failed=0
        while read -r pkg; do
          case "$pkg" in *"$pkg_filter"*) ;; *) continue ;; esac
          listed="$(go test -list '^Fuzz' "$pkg")"
          while read -r target; do
            if [ -n "$target_filter" ] && [ "$target" != "$target_filter" ]; then continue; fi
            ran=$((ran + 1))
            echo "== fuzz $pkg $target for $fuzztime"
            if ! go test -run '^$' -fuzz "^''${target}\$" -fuzztime "$fuzztime" -parallel "$procs" "$pkg"; then
              failed=$((failed + 1))
              echo "!! $target failed; the crasher is under $(go list -f '{{.Dir}}' "$pkg")/testdata/fuzz/$target" >&2
            fi
          done < <(grep '^Fuzz' <<< "$listed" || true)
        done < <(go list ./...)
        echo "fuzz: $ran target(s), $failed failed"
        if [ "$ran" -eq 0 ]; then
          echo "no fuzz target matched" >&2
          exit 1
        fi
        [ "$failed" -eq 0 ]
      '';
    };
    bench = {
      group = "fuzz";
      description = "benchmarks, -count 6, into bench.txt";
      runtimeInputs = goTools;
      text = ''go test -run '^$' -bench . -benchmem -count "''${COUNT:-6}" "$@" ./... | tee "''${OUT:-bench.txt}"'';
    };
    bench-compare = {
      group = "fuzz";
      description = "benchstat: working tree vs origin/main (or \$BASE)";
      runtimeInputs = goTools ++ [
        versions.goperf
        pkgs.git
      ];
      text = ''
        base="''${BASE:-origin/main}" count="''${COUNT:-6}"
        tmp="$(mktemp -d)"
        trap 'git worktree remove --force "$tmp/base" >/dev/null 2>&1 || true; rm -rf "$tmp"' EXIT
        git worktree add --detach "$tmp/base" "$base" >/dev/null
        (cd "$tmp/base" && go test -run '^$' -bench . -benchmem -count "$count" ./...) > "$tmp/old.txt"
        go test -run '^$' -bench . -benchmem -count "$count" ./... > "$tmp/new.txt"
        benchstat "$tmp/old.txt" "$tmp/new.txt"
      '';
    };

    # ---- maintenance --------------------------------------------------------
    update-vendor-hash = {
      group = "nix";
      description = "refresh vendorHash in nix/versions.nix after a go.mod/go.sum change";
      runtimeInputs = [ pkgs.gnused ];
      text = ''
        log="$(mktemp)"
        if nix build --no-link .#runpodctl.goModules 2> "$log"; then
          echo "vendorHash is up to date"
          exit 0
        fi
        got="$(sed -n 's/^ *got: *\(sha256-[A-Za-z0-9+/=]*\).*/\1/p' "$log" | head -n1)"
        if [ -z "$got" ]; then
          cat "$log" >&2
          echo "the build failed for a reason other than the hash" >&2
          exit 1
        fi
        sed -i "s|vendorHash = \"sha256-[A-Za-z0-9+/=]*\"|vendorHash = \"$got\"|" nix/versions.nix
        echo "vendorHash -> $got"
        nix build --no-link .#runpodctl.goModules
      '';
    };
    test-integration = {
      group = "nix";
      description = "exercise every flake target (nix/tests/integration.sh)";
      runtimeInputs = [
        pkgs.jq
        pkgs.file
      ];
      text = tests.integration;
    };
  }
  // lib.optionalAttrs (images != { }) {
    oci-size-report = {
      group = "oci";
      description = "size of each image (markdown table)";
      text = ''
        echo "| Image | Bytes | MiB |"
        echo "| --- | ---: | ---: |"
        ${lib.concatStrings (
          lib.mapAttrsToList (name: image: ''
            bytes="$(${image} | wc -c)"
            echo "| ${name} | $bytes | $(( bytes / 1048576 )) |"
          '') images
        )}
      '';
    };
    oci-push = {
      group = "oci";
      description = "push this system's images to \$REGISTRY (e.g. ghcr.io/runpod)";
      runtimeInputs = [ pkgs.skopeo ];
      text = ''
        : "''${REGISTRY:?set REGISTRY, e.g. REGISTRY=ghcr.io/runpod}"
        arch="${pkgs.go.GOARCH}"
        tmp="$(mktemp -d)"
        trap 'rm -rf "$tmp"' EXIT
        ${lib.concatStrings (
          lib.mapAttrsToList (_: image: ''
            ${image} > "$tmp/image.tar"
            skopeo copy "docker-archive:$tmp/image.tar" "docker://$REGISTRY/${image.imageName}:${image.imageTag}-$arch"
          '') images
        )}
      '';
    };
  };
in
lib.mapAttrs (
  name: entry:
  pkgs.writeShellApplication {
    inherit name;
    runtimeInputs = import ../lib/base-tools.nix pkgs ++ entry.runtimeInputs or [ ];
    text = prelude + entry.text;
    meta = { inherit (entry) description; };
    passthru = { inherit (entry) group; };
  }
) table
