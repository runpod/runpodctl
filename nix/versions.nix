#
# nix/versions.nix — runpodctl
#
# Single source of truth for every version the flake pins: the Go toolchain,
# the module-cache hash, the version stamped into the binary, the analysis
# tools and the fuzzing budgets. Every other module imports this file instead
# of reaching into pkgs for a tool, so a bump is one edit here.
#
# Go is overridden rather than taken from nixpkgs because nixpkgs lags point
# releases, and point releases are where Go ships its security fixes (the
# nixos-unstable this flake was written against carried 1.26.8, which has ten
# advisories fixed in 1.26.9). The `go` directive in go.mod is a *minimum*, so
# the assertion below only requires the flake's Go to be at least that; it
# fails evaluation of anything that uses Go when go.mod moves past it.
#
{ pkgs, lib }:
let
  readMatch =
    file: regex: what:
    let
      m = builtins.match regex (builtins.readFile file);
    in
    if m == null then
      throw "nix/versions.nix: could not parse ${what} from ${toString file}; update the regex there"
    else
      builtins.head m;

  # go.mod's `go` directive, e.g. "1.26.6".
  goModVersion = readMatch ../go.mod ".*\ngo ([0-9.]+)\n.*" "the go directive";

  # main.go's default version, e.g. "v3.0.0"; -X main.Version overrides it.
  baseVersion = readMatch ../main.go ".*\nvar Version = \"([^\"]+)\"\n.*" "var Version";

  pinnedGo = pkgs.go_1_26.overrideAttrs (_old: rec {
    version = "1.26.9";
    src = pkgs.fetchurl {
      url = "https://go.dev/dl/go${version}.src.tar.gz";
      hash = "sha256-lzXX3Ntls10/pXfwQGRzfAO4nPGitx5uaf4vPG+f1Mo=";
    };
  });
  # The assertion guards `go` alone, not the whole set: `nix run .#update-pins`
  # must still evaluate when go.mod has moved past the pin, since that is what
  # rewrites it.
  go =
    assert lib.assertMsg (lib.versionAtLeast pinnedGo.version goModVersion)
      "go.mod requires go ${goModVersion} but nix/versions.nix pins ${pinnedGo.version}; run `nix run .#update-pins`";
    pinnedGo;
in
{
  inherit go goModVersion baseVersion;

  # Hash of the proxyVendor module cache (`goModules`). Any go.mod/go.sum
  # change invalidates it: run `nix run .#update-vendor-hash`.
  vendorHash = "sha256-euC7FNi6GTu5W6DE/SPuywtnoM8an6etlcHJbQBxxeQ=";

  # Identical to .goreleaser.yml, minus the version, which the package adds.
  ldflags = [
    "-s"
    "-w"
  ];

  # Analysis tools come straight from nixpkgs (and so from cache.nixos.org).
  # They only need the same Go *language* version (1.26) as the module; the
  # pinned `go` above is first on PATH in every check, so package loading and
  # govulncheck's standard-library check both see 1.26.9.
  inherit (pkgs)
    golangci-lint
    gosec
    govulncheck
    deadnix
    statix
    shellcheck
    actionlint
    goperf
    nixfmt
    ;
  staticcheck = pkgs.go-tools;

  # Fuzzing budgets per target: CI on pull requests, the nightly run, and the
  # default for `nix run .#fuzz`.
  fuzzTime = {
    ci = "30s";
    nightly = "10m";
    local = "60s";
  };

  # Systems that build OCI images (each builds its own architecture natively).
  ociSystems = [
    "x86_64-linux"
    "aarch64-linux"
  ];
}
