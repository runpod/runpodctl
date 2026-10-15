#
# nix/default.nix — runpodctl
#
# Per-system aggregator: wires the modules under ./nix together and returns
# the flake's `packages`, `devShells`, `checks`, `apps` and `formatter`.
#
# Everything is plain `import ./x.nix { inherit … }`, never callPackage, so
# every argument a module receives is visible here. Policy (which checks are
# gated, which systems build images) is decided here and passed in as data.
#
{
  pkgs,
  lib,
  src,
  rev,
}:
let
  inherit (pkgs.stdenv.hostPlatform) system isLinux;

  versions = import ./versions.nix { inherit pkgs lib; };
  sources = import ./source.nix { inherit lib src; };

  runpodctl = import ./packages/runpodctl.nix {
    inherit
      pkgs
      lib
      versions
      rev
      ;
    src = sources.goSrc;
  };

  mkCheck = import ./lib/mk-check.nix { inherit pkgs lib; };
  mkGoCheck = import ./lib/mk-go-check.nix {
    inherit pkgs lib versions;
    inherit (runpodctl) goModules;
  };

  staticAnalysis = import ./static-analysis {
    inherit
      lib
      versions
      sources
      mkGoCheck
      mkCheck
      runpodctl
      rev
      ;
  };

  checks = import ./checks {
    inherit
      lib
      pkgs
      versions
      sources
      mkGoCheck
      mkCheck
      runpodctl
      rev
      ;
    inherit (staticAnalysis) lint-baseline;
  };

  # OCI images only on the linux systems that build them natively.
  images = lib.optionalAttrs (builtins.elem system versions.ociSystems) (
    import ./containers {
      inherit
        pkgs
        lib
        runpodctl
        rev
        ;
    }
  );

  commands = import ./dev/commands.nix {
    inherit
      pkgs
      lib
      versions
      staticAnalysis
      ;
    inherit images;
    tests.integration = builtins.readFile ./tests/integration.sh;
  };
in
{
  packages = {
    default = runpodctl;
    inherit runpodctl;
    inherit (staticAnalysis) staticreport static-analysis-measure static-analysis-report;
  }
  # Built on demand, never by `nix flake check` (see nix/checks/default.nix).
  // lib.optionalAttrs isLinux checks.ungated
  // images;

  checks = checks.gated;

  devShells.default = import ./dev/shell.nix {
    inherit
      pkgs
      lib
      versions
      commands
      ;
  };

  apps = import ./apps { inherit lib commands runpodctl; };

  # treefmt + nixfmt over the whole tree; a bare nixfmt would read stdin.
  formatter = pkgs.nixfmt-tree;
}
