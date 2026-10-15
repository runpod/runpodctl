#
# flake.nix — runpodctl
#
# Thin orchestrator. Every real decision lives under ./nix (see
# nix/design.md for why, and nix/README.md for how to use it). This file only
# names the inputs, the supported systems and the exported outputs.
#
# Systems: x86_64-linux, aarch64-linux, aarch64-darwin. Targets marked
# (linux) exist only on the linux systems. `nix flake show` lists them all.
#
# ---- use the cli ------------------------------------------------------------
#
#   nix run github:runpod/runpodctl -- --help      run the cli without a clone
#   nix profile install github:runpod/runpodctl   install it onto your PATH
#   nix run . -- pod list                          run it from a clone
#
# ---- packages: nix build .#<name> (output in ./result) ----------------------
#
#   runpodctl (default)       the cli, built as goreleaser builds it
#   oci-runpodctl             (linux) image: the cli + ca certs, uid 65532
#   oci-runpodctl-full        (linux) the same + rsync, openssh, coreutils
#                               load either with: ./result | docker load
#   static-analysis-measure   every static analysis tool's raw output, run once
#   static-analysis-report    report.md + report.json built from the measure
#   staticreport              the go tool behind the baseline and the report
#   go-test-race              (linux) the race detector; not gated, see
#                               nix/checks/default.nix
#
# ---- checks: nix flake check -L, or nix build .#checks.<system>.<name> ------
#
#   go-vet          go vet ./..., plain and -tags e2e
#   gofmt           gofmt -l . prints nothing
#   go-mod-verify   go mod verify against the module cache
#   go-test         go test -count=1 -cover ./..., with real openssh and rsync
#   fuzz-seeds      every Fuzz* target's seeds and committed crashers
#   cli-help-smoke  --help for every command; no error object on stdout
#   version-stamp   the binary reports the flake's commit
#   lint-baseline   no new finding, and no fixed one still listed
#   nixfmt deadnix statix   the nix files
#   shellcheck      every *.sh, and no `# shellcheck disable` anywhere
#   actionlint      every workflow
#
# ---- dev shell: nix develop -------------------------------------------------
#
#   pinned go, golangci-lint, staticcheck, gosec, govulncheck, gopls, delve,
#   shellcheck, the nix linters and benchstat. every app below is also a
#   command in the shell (runpodctl-help lists them); outside it, run them as
#   nix run .#<name>. they run in the working tree, from the repo root.
#
# ---- apps: nix run .#<name> -------------------------------------------------
#
#   check     check-vet            go vet, with and without the e2e build tag
#             check-fmt            list files gofmt would change
#             check-tidy           go mod tidy -diff (needs network)
#             check-test           unit tests with coverage
#             check-race           unit tests under the race detector
#             check-all            every hermetic check: nix flake check -L
#   lint      lint-quick           golangci-lint tier 0 (correctness)
#             lint                 golangci-lint tier 1 (standard)
#             lint-comprehensive   golangci-lint tier 2 (comprehensive)
#             lint-fix             apply tier 1 autofixes to the working tree
#             check-staticcheck    staticcheck -checks all
#             check-gosec          gosec security scan
#             govulncheck          reachable known vulns (needs network)
#             static-analysis-report        print the hermetic report
#             static-analysis-report-full   the report + govulncheck
#                                           -> ./static-analysis-report/
#             update-lint-baseline rewrite nix/checks/lint-baseline.txt after
#                                  fixing findings
#   fuzz      fuzz                 long fuzzing: [--fuzztime 60s | --budget
#                                  ci|nightly] [--pkg SUBSTR] [--target NAME]
#             bench                benchmarks, -count 6, into bench.txt
#             bench-compare        benchstat: working tree vs origin/main
#   nix       update-vendor-hash   refresh vendorHash after go.mod/go.sum edits
#             test-integration     exercise every flake target
#                                  (nix/tests/integration.sh)
#   oci       oci-size-report      (linux) size of each image, as markdown
#             oci-push             (linux) push the images to $REGISTRY
#   cli       runpodctl (default)  the cli itself
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
