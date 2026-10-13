#
# nix/packages/runpodctl.nix — runpodctl
#
# The CLI, built hermetically with the same flags goreleaser uses
# (.goreleaser.yml): CGO off, `-s -w`, `-X main.Version=<version>-<commit>`.
#
# `proxyVendor = true` makes `goModules` a GOPROXY-format module cache instead
# of a vendor/ tree. The repo has no vendor/ directory and always builds with
# -mod=mod, so this matches how runpodctl is built today, and the Go checks
# reuse the same cache (`passthru.goModules`) with GOPROXY=file://… to run
# offline, including `go mod verify`.
#
# Tests are separate checks (nix/checks), so `doCheck = false`: a test edit
# re-runs the tests without rebuilding the package.
#
{
  pkgs,
  lib,
  versions,
  src,
  rev,
}:
let
  buildGoModule = pkgs.buildGoModule.override { inherit (versions) go; };
in
buildGoModule {
  pname = "runpodctl";
  # The derivation version is the base version only, so the module cache
  # (named after it) stays the same store path across commits; the commit is
  # stamped through ldflags, as goreleaser does.
  version = versions.baseVersion;
  inherit src;
  inherit (versions) vendorHash;
  proxyVendor = true;

  subPackages = [ "." ];
  env.CGO_ENABLED = "0";
  ldflags = versions.ldflags ++ [ "-X main.Version=${versions.baseVersion}-${rev}" ];

  preBuild = ''
    export GOFLAGS="-trimpath ''${GOFLAGS:-}"
  '';

  doCheck = false;

  passthru = {
    inherit (versions) go;
  };

  meta = {
    description = "Runpod command line interface";
    homepage = "https://github.com/runpod/runpodctl";
    license = lib.licenses.gpl3Only;
    mainProgram = "runpodctl";
    platforms = [
      "x86_64-linux"
      "aarch64-linux"
      "aarch64-darwin"
    ];
  };
}
