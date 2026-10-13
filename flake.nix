#
# flake.nix — runpodctl
#
# Thin orchestrator. Every real decision lives under ./nix (see
# nix/design.md for why, and nix/README.md for how to use it). This file only
# names the inputs, the supported systems and the exported outputs.
#
# Quick reference:
#
#   nix run github:runpod/runpodctl -- --help     run the cli without a clone
#   nix profile install github:runpod/runpodctl  install it
#   nix develop                                   dev shell (go, linters, fuzzing)
#   nix build                                     build the binary -> ./result
#   nix flake check -L                            hermetic checks (vet, test, lint ratchet, ...)
#   nix build .#oci-runpodctl && ./result | docker load     slim image (linux)
#   nix build .#static-analysis-report            strict static analysis report
#   nix run .#static-analysis-report-full         ... plus govulncheck (needs network)
#   nix run .#fuzz -- --fuzztime 60s              long fuzzing (writes crashers to testdata/fuzz)
#   nix run .#test-integration                    exercise every flake target
#
{
  description = "runpodctl — the Runpod command line interface";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
    }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "aarch64-darwin"
      ];

      # The commit the binary is stamped with; mirrors goreleaser's
      # {{ .ShortCommit }}. A dirty tree is stamped as such rather than hidden.
      rev = self.shortRev or self.dirtyShortRev or "unknown";
    in
    flake-utils.lib.eachSystem systems (
      system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        outputs = import ./nix {
          inherit pkgs rev;
          inherit (nixpkgs) lib;
          src = ./.;
        };
      in
      {
        inherit (outputs)
          packages
          devShells
          checks
          apps
          formatter
          ;
      }
    )
    // {
      overlays.default = import ./nix/overlays.nix { inherit self; };
    };
}
