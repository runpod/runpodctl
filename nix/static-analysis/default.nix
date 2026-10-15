#
# nix/static-analysis/default.nix — runpodctl
#
# The pedantic static analysis layer (design §7): every tool runs exactly
# once, in `static-analysis-measure`, and two consumers read that one result:
#
#   - `lint-baseline` (a gated check) compares it against the committed
#     ratchet, nix/checks/lint-baseline.txt. A new finding fails, and so does
#     a fixed one that is still listed, so the file only ever shrinks.
#   - `static-analysis-report` (a package) renders it as Markdown + JSON.
#
# The measure always succeeds and records each tool's exit status next to its
# JSON, so a finding is data, not a build failure; a tool that *crashed* (exit
# above its findings status) is caught by staticreport, which refuses to treat
# incomplete output as "no findings".
#
# govulncheck needs the live vulnerability database, so it can't run here.
# `nix run .#static-analysis-report-full` (nix/apps) adds it outside the
# sandbox.
#
# The tools are listed once, in `tools`; every command line (the measure, the
# check, the report, the updater app) is generated from that list.
#
{
  lib,
  versions,
  sources,
  mkGoCheck,
  mkCheck,
  runpodctl,
  rev,
}:
let
  # name -> { format; run; }: `run` writes "$raw/<name>.json".
  tools = {
    golangci-tier0 = {
      format = "golangci";
      run = ''golangci-lint run --config .golangci-quick.yml --output.json.path "$raw/golangci-tier0.json" ./...'';
    };
    golangci-tier1 = {
      format = "golangci";
      run = ''golangci-lint run --config .golangci.yml --output.json.path "$raw/golangci-tier1.json" ./...'';
    };
    golangci-tier2 = {
      format = "golangci";
      run = ''golangci-lint run --config .golangci-comprehensive.yml --output.json.path "$raw/golangci-tier2.json" ./...'';
    };
    staticcheck = {
      format = "staticcheck";
      run = ''staticcheck -checks all -tags e2e -f json ./... > "$raw/staticcheck.json"'';
    };
    gosec = {
      format = "gosec";
      run = ''gosec -quiet -no-fail -tags e2e -fmt json -out "$raw/gosec.json" ./...'';
    };
  };

  # The report tool, built from this repo (tools/staticreport) with the same
  # module cache as the CLI.
  # Without the commit in its ldflags, so it isn't rebuilt on every commit.
  staticreport = runpodctl.overrideAttrs (_: {
    pname = "runpodctl-staticreport";
    subPackages = [ "tools/staticreport" ];
    inherit (versions) ldflags;
    meta.mainProgram = "staticreport";
  });

  measure = mkGoCheck {
    name = "static-analysis-measure";
    src = sources.analysisSrc;
    runtimeInputs = [
      versions.golangci-lint
      versions.staticcheck
      versions.gosec
    ];
    script = ''
      raw="$out/raw"
      mkdir -p "$raw" "$out/meta"
      pwd > "$out/root.txt"
      {
        echo "go=$(go env GOVERSION)"
        echo "golangci-lint=$(golangci-lint version --short)"
        echo "staticcheck=$(staticcheck -version)"
        echo "gosec=${versions.gosec.version}"
      } > "$out/meta/versions.txt"
    ''
    + lib.concatStrings (
      lib.mapAttrsToList (name: tool: ''
        echo "== ${name}"
        rc=0
        # Grouped, so a `run` that redirects its own stdout to the JSON
        # keeps it; only what's left goes to the log.
        { ${tool.run}; } > "$raw/${name}.log" 2>&1 || rc=$?
        echo "$rc" > "$raw/${name}.exit"
        echo "exit $rc"
      '') tools
    );
  };

  # `-in`/`-exit` flags for every tool, reading the measure at `m`.
  toolArgs =
    m:
    lib.concatStringsSep " " (
      lib.mapAttrsToList (
        name: tool:
        ''-in ${name}=${tool.format}:${m}/raw/${name}.json -exit ${name}="$(cat ${m}/raw/${name}.exit)"''
      ) tools
    );

  lint-baseline = mkCheck {
    name = "lint-baseline";
    src = sources.baselineSrc;
    runtimeInputs = [ staticreport ];
    script = ''
      staticreport baseline -baseline nix/checks/lint-baseline.txt \
        -root "$(cat ${measure}/root.txt)" ${toolArgs measure}
    '';
  };

  report = mkCheck {
    name = "static-analysis-report";
    src = sources.baselineSrc;
    runtimeInputs = [ staticreport ];
    script = ''
      cp -r ${measure}/raw "$out/raw"
      # The commit is added here, not in the measure, so a new commit with no
      # Go change reuses the cached measure instead of re-running every tool.
      { echo "rev=${rev}"; cat ${measure}/meta/versions.txt; } > "$out/meta.txt"
      staticreport report -baseline nix/checks/lint-baseline.txt \
        -root "$(cat ${measure}/root.txt)" ${toolArgs measure} -meta-file "$out/meta.txt" \
        -md "$out/report.md" -json "$out/report.json"
      echo "wrote $out/report.md"
    '';
  };
in
{
  inherit staticreport toolArgs;
  static-analysis-measure = measure;
  static-analysis-report = report;
  inherit lint-baseline;
}
