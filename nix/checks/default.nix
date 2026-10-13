#
# nix/checks/default.nix — runpodctl
#
# The checks `nix flake check` runs: hermetic, offline, and independent of the
# machine. Each Go check is one row in `goChecks` (mk-go-check); each check of
# the built binary or of a non-Go file type is one row in `fileChecks`
# (mk-check). Each row names the fileset it reads (nix/source.nix), so editing a
# workflow re-runs only actionlint and editing a `.nix` file only the Nix
# linters.
#
# Returns `gated` (the flake's `checks`) and `ungated` (built on demand, never
# by `nix flake check`). The only ungated check is the race detector:
# cmd/transfer's tests race inside upstream croc's global logger
# (`tcp.Run` calls `logger.SetLevel` while `tcp.PingServer` reads it), so it is
# gated once that's fixed (design §7).
#
{
  pkgs,
  lib,
  versions,
  sources,
  mkGoCheck,
  mkCheck,
  runpodctl,
  rev,
  lint-baseline,
}:
let
  # Real ssh/rsync on PATH, so the tests that drive both binaries
  # (cmd/project/hostkey_test.go) run instead of skipping.
  testRuntime = [
    pkgs.openssh
    pkgs.rsync
  ];

  goChecks = {
    go-vet = {
      # e2e/ is behind //go:build e2e: vetted, never run.
      script = ''
        go vet ./...
        go vet -tags e2e ./...
      '';
    };

    gofmt.script = ''
      unformatted="$(gofmt -l .)"
      if [ -n "$unformatted" ]; then
        echo "not gofmt-formatted (run gofmt -w):"
        echo "$unformatted"
        exit 1
      fi
    '';

    go-mod-verify.script = ''
      go mod download
      go mod verify
    '';

    go-test = {
      runtimeInputs = testRuntime;
      script = "go test -count=1 -cover ./...";
    };

    # Every Fuzz* target's seed corpus plus its committed testdata/fuzz/
    # crashers, i.e. deterministic regression tests. Targets are discovered,
    # never listed, so adding a Fuzz* test is the only step (design §8).
    fuzz-seeds = {
      runtimeInputs = testRuntime;
      script = ''
        found=0
        while read -r pkg; do
          # listed into a variable, not a pipe, so a package whose tests
          # don't compile fails here instead of reporting no targets.
          listed="$(go test -list '^Fuzz' "$pkg")"
          while read -r target; do
            found=$((found + 1))
            echo "== $pkg $target"
            go test -count=1 -run "^''${target}\$" "$pkg"
          done < <(grep '^Fuzz' <<< "$listed" || true)
        done < <(go list ./...)
        echo "fuzz-seeds: $found target(s)"
      '';
    };
  };

  # Each row names why it is not gated; the integration test reads it
  # (passthru.ungatedReason) and expects the failure until it is fixed.
  ungatedGoChecks = {
    go-test-race = {
      ungatedReason = "cmd/transfer's tests race inside upstream croc's global logger";
      cgo = true;
      runtimeInputs = testRuntime;
      script = "go test -race -count=1 ./...";
    };
  };

  bin = lib.getExe runpodctl;

  fileChecks = {
    # The built binary's top-level help, `version`, and `<cmd> --help` for
    # every command it lists. Commands are read from the help output, so a new
    # command is covered without editing this file. stdout must never carry an
    # error object (CON-683).
    cli-help-smoke.script = ''
      ${bin} --help > help.txt
      test -s help.txt
      ${bin} version
      mapfile -t cmds < <(sed -n '/^Available Commands:/,/^$/{/^  [a-z]/{s/^  \([a-z-]*\).*/\1/;p}}' help.txt)
      if [ "''${#cmds[@]}" -lt 10 ]; then
        echo "found only ''${#cmds[@]} commands in --help; did the help format change?"
        exit 1
      fi
      for cmd in "''${cmds[@]}"; do
        ${bin} "$cmd" --help > "help-$cmd.txt"
        test -s "help-$cmd.txt"
        if grep -q '"error"' "help-$cmd.txt"; then
          echo "$cmd --help printed an error object on stdout"
          exit 1
        fi
        echo "ok $cmd"
      done
      echo "cli-help-smoke: ''${#cmds[@]} commands"
    '';

    # The binary reports the commit the flake was built from.
    version-stamp.script = ''
      got="$(${bin} version)"
      echo "$got"
      case "$got" in
        *"${versions.baseVersion}-${rev}"*) ;;
        *) echo "expected version ${versions.baseVersion}-${rev}"; exit 1 ;;
      esac
    '';

    nixfmt = {
      src = sources.nixSrc;
      runtimeInputs = [ versions.nixfmt ];
      script = "find . -name '*.nix' -print0 | xargs -0 -r nixfmt --check";
    };

    deadnix = {
      src = sources.nixSrc;
      runtimeInputs = [ versions.deadnix ];
      script = "deadnix --fail .";
    };

    statix = {
      src = sources.nixSrc;
      runtimeInputs = [ versions.statix ];
      script = "statix check .";
    };

    # Every severity (style included), and no `# shellcheck disable` anywhere:
    # findings are fixed, not suppressed. `-o all` (the optional checks) is a
    # follow-up: it adds ~150 findings to the public installer, mostly
    # SC2250 brace style plus set -e restructuring (SC2310/2311) that
    # deserves its own reviewed change.
    shellcheck = {
      src = sources.shSrc;
      runtimeInputs = [ versions.shellcheck ];
      script = ''
        if grep -rn 'shellcheck disable' --include='*.sh' .; then
          echo "shellcheck findings must be fixed, not disabled"
          exit 1
        fi
        find . -name '*.sh' -print0 | xargs -0 -r shellcheck --severity=style
      '';
    };

    actionlint = {
      src = sources.workflowSrc;
      runtimeInputs = [
        versions.actionlint
        versions.shellcheck
      ];
      script = "actionlint -shellcheck=shellcheck .github/workflows/*.yml";
    };
  };

  mkGo = lib.mapAttrs (
    name: row:
    mkGoCheck (
      {
        inherit name;
        src = sources.goSrc;
      }
      // row
    )
  );
  mkFile = lib.mapAttrs (name: row: mkCheck ({ inherit name; } // row));
in
{
  gated =
    mkGo goChecks
    // mkFile fileChecks
    // {
      inherit lint-baseline;
    };

  ungated = lib.mapAttrs (
    name: drv:
    drv.overrideAttrs (old: {
      passthru = old.passthru // {
        inherit (ungatedGoChecks.${name}) ungatedReason;
      };
    })
  ) (mkGo (lib.mapAttrs (_: row: removeAttrs row [ "ungatedReason" ]) ungatedGoChecks));
}
