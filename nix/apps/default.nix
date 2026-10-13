#
# nix/apps/default.nix — runpodctl
#
# `nix run .#<name>`: one app per entry in nix/dev/commands.nix (the same
# scripts the dev shell puts on PATH), plus `runpodctl` itself. Apps are for
# what a check can't be: anything that needs the network (govulncheck, fuzzing
# for minutes), writes to the working tree (update-*, fuzz crashers) or
# depends on the machine (benchmarks).
#
{
  lib,
  commands,
  runpodctl,
}:
let
  app = drv: description: {
    type = "app";
    program = lib.getExe drv;
    meta = { inherit description; };
  };
in
lib.mapAttrs (_: drv: app drv drv.meta.description) commands
// {
  default = app runpodctl "the runpodctl cli";
  runpodctl = app runpodctl "the runpodctl cli";
}
