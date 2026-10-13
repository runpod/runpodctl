#
# nix/overlays.nix — runpodctl
#
# `overlays.default`: adds `pkgs.runpodctl` for NixOS / home-manager users who
# want the CLI from their own nixpkgs instance. It reuses this flake's package
# for the overlaid system, so the binary is identical to `nix build`'s.
#
{ self }:
final: _prev: {
  runpodctl = self.packages.${final.stdenv.hostPlatform.system}.runpodctl;
}
