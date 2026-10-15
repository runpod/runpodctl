#
# nix/lib/mk-go-check.nix — runpodctl
#
# mk-check plus the one Go environment every Go check shares, so none of them
# can drift:
#
#   - HOME, GOPATH, GOCACHE and GOMODCACHE under the build's temp dir. HOME
#     matters beyond Go: `initConfig` writes ~/.runpod/config.toml whenever a
#     test Execute()s a command, so a check must never see a real home;
#   - modules from the package's proxyVendor cache via GOPROXY=file://…, with
#     GOFLAGS=-mod=mod and GOSUMDB=off (go.sum still verifies every module),
#     and GOTOOLCHAIN=local so Go never tries to download a toolchain;
#   - the pinned Go first on PATH, and its version in $out/go-version.txt;
#   - on darwin, loopback networking, which ~35 test files need.
#
{
  pkgs,
  lib,
  versions,
  goModules,
}:
let
  mkCheck = import ./mk-check.nix { inherit pkgs lib; };
in
{
  name,
  src,
  script,
  runtimeInputs ? [ ],
  cgo ? false,
}:
mkCheck {
  inherit name src script;
  runtimeInputs = [ versions.go ] ++ lib.optional cgo pkgs.stdenv.cc ++ runtimeInputs;
  preamble = ''
    export GOPATH="$work/go" GOCACHE="$work/go-build" GOMODCACHE="$work/go/pkg/mod"
    export GOPROXY="file://${goModules}" GOFLAGS="-mod=mod" GOSUMDB=off
    export GOTOOLCHAIN=local CGO_ENABLED=${if cgo then "1" else "0"}
    go version > "$out/go-version.txt"
  '';
  drvAttrs.__darwinAllowLocalNetworking = true;
}
