#
# nix/dev/shell.nix — runpodctl
#
# `nix develop`: the pinned Go, every analysis tool, the runtime deps the
# tests drive (openssh, rsync), and every helper from ./commands.nix on PATH.
# The banner is rendered from the same table, so it always lists exactly the
# helpers that exist.
#
# The shellHook only prints the banner and sets CGO_ENABLED/GOTOOLCHAIN; no
# other environment magic.
#
{
  pkgs,
  lib,
  versions,
  commands,
}:
let
  groups = lib.groupBy (drv: drv.passthru.group) (lib.attrValues commands);

  banner = pkgs.writeShellApplication {
    name = "runpodctl-help";
    text = ''
      cat <<'EOF'
      runpodctl dev shell — go ${versions.go.version}

      ${lib.concatStringsSep "\n\n" (
        lib.mapAttrsToList (
          group: drvs:
          "${group}:\n"
          + lib.concatMapStringsSep "\n" (
            drv: "  ${lib.fixedWidthString 28 " " drv.name} ${drv.meta.description}"
          ) drvs
        ) groups
      )}

      runpodctl-help prints this again. More: nix/README.md
      EOF
    '';
  };
in
pkgs.mkShell {
  name = "runpodctl-dev";

  packages = [
    versions.go
    pkgs.gopls
    pkgs.gotools
    pkgs.delve
    versions.golangci-lint
    versions.staticcheck
    versions.gosec
    versions.govulncheck
    versions.goperf
    pkgs.openssh
    pkgs.rsync
    versions.shellcheck
    versions.actionlint
    versions.nixfmt
    versions.deadnix
    versions.statix
    pkgs.jq
    pkgs.skopeo
    banner
  ]
  ++ lib.attrValues commands;

  shellHook = ''
    export CGO_ENABLED=0 GOTOOLCHAIN=local
    runpodctl-help
  '';
}
