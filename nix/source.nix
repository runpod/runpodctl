#
# nix/source.nix — runpodctl
#
# The `lib.fileset` sources every derivation builds from. Each one is the
# smallest set of files that can change its result, so editing a Markdown doc
# rebuilds nothing, editing a workflow re-runs only actionlint, and editing a
# `.nix` file re-runs only the Nix linters.
#
# `goSrc` includes cmd/croc/relays.json and test data alongside the go files.
#
{ lib, src }:
let
  fs = lib.fileset;

  # Only files git tracks, so build outputs (./result, coverage files, local
  # binaries) never leak into a derivation. Falls back to the whole tree when
  # evaluated outside a git checkout (e.g. a tarball).
  tracked = if builtins.pathExists (src + "/.git") then fs.gitTracked src else src;
  inTree = files: fs.intersection tracked files;

  toSource =
    fileset:
    fs.toSource {
      root = src;
      inherit fileset;
    };

  goFiles = inTree (
    fs.unions [
      (fs.fileFilter (f: f.hasExt "go") src)
      (src + "/go.mod")
      (src + "/go.sum")
      (src + "/cmd/croc/relays.json")
      testdata
    ]
  );

  # Every `testdata/` directory (fuzz corpora under testdata/fuzz/Fuzz*/ have
  # extensionless names, so a fileFilter on the name can't find them).
  testdata = fs.fromSource (
    lib.cleanSourceWith {
      inherit src;
      filter = path: type: type == "directory" || lib.hasInfix "/testdata/" (toString path);
    }
  );

  golangciConfigs = inTree (fs.fileFilter (f: lib.hasPrefix ".golangci" f.name) src);
  baseline = fs.maybeMissing (src + "/nix/checks/lint-baseline.txt");
in
{
  # The full Go module: build, vet, test, fuzz seeds.
  goSrc = toSource goFiles;

  # What the analysis tools read: the Go module + the golangci-lint tier
  # configs. Not the baseline, so recording a shrunk baseline doesn't re-run
  # every linter.
  analysisSrc = toSource (fs.union goFiles golangciConfigs);

  # Just the committed lint ratchet, for the check that compares against it.
  baselineSrc = toSource baseline;

  nixSrc = toSource (inTree (fs.fileFilter (f: f.hasExt "nix") src));
  shSrc = toSource (inTree (fs.fileFilter (f: f.hasExt "sh") src));
  workflowSrc = toSource (inTree (src + "/.github/workflows"));
}
