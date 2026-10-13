#
# nix/lib/mk-check.nix — runpodctl
#
# Turns one check into a shellchecked `writeShellApplication` plus a one-line
# runner (the flash pattern). All the logic lives in the application text, so:
#
#   - shellcheck runs over it at build time (writeShellApplication's default
#     checkPhase), which the plain `runCommand "…''<blob>''"` idiom lacks;
#   - it runs under `set -o errexit -o nounset -o pipefail`;
#   - PATH is exactly `runtimeInputs` (+ coreutils & co), so a check can't
#     silently depend on something installed on the machine.
#
# The check runs in a writable copy of its fileset-scoped `src` (an empty
# directory when `src` is null, for checks of the built binary). Its combined
# output goes to $out/output.txt and the script itself to $out/command.sh, so a
# cached result records how it was made; on failure the output is printed and
# the exit code is kept. Output is captured by redirecting the shell itself
# rather than `script || rc=$?`, which would switch errexit off for the script.
#
# `preamble` runs before the capture starts (mk-go-check uses it to set up the
# Go environment); `drvAttrs` are merged into the runner derivation.
#
{ pkgs, lib }:
{
  name,
  src ? null,
  script,
  runtimeInputs ? [ ],
  preamble ? "",
  drvAttrs ? { },
}:
let
  scriptFile = pkgs.writeText "runpodctl-${name}-command.sh" script;

  app = pkgs.writeShellApplication {
    name = "runpodctl-${name}";
    runtimeInputs = import ./base-tools.nix pkgs ++ runtimeInputs;
    text = ''
      out="$1"
      mkdir -p "$out"
      cp ${scriptFile} "$out/command.sh"

      work="$(mktemp -d)"
      mkdir -p "$work/src"
      ${lib.optionalString (src != null) ''
        cp -r ${src}/. "$work/src"
        chmod -R +w "$work/src"
      ''}
      cd "$work/src"
      export HOME="$work/home"
      mkdir -p "$HOME"

      ${preamble}

      # On failure, replay the captured output to the build log.
      on_exit() {
        local rc=$?
        if [ "$rc" -ne 0 ]; then
          cat "$out/output.txt" >&3
          echo "check ${name} failed (exit $rc)" >&3
        fi
      }
      exec 3>&2
      exec > "$out/output.txt" 2>&1
      trap on_exit EXIT

      ${script}
    '';
  };
in
pkgs.runCommand "runpodctl-${name}" (
  {
    passthru = { inherit app; };
  }
  // drvAttrs
) ''${lib.getExe app} "$out"''
