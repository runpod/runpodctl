#
# nix/lib/base-tools.nix — runpodctl
#
# The userland every script gets ahead of its own runtimeInputs: checks
# (./mk-check.nix) and the dev commands (../dev/commands.nix). Pinning GNU
# tools keeps scripts portable to darwin, whose /bin/cp and /usr/bin/sed are
# BSD and reject flags like `cp --no-preserve` or `sed -i` without a suffix.
#
pkgs: [
  pkgs.coreutils
  pkgs.findutils
  pkgs.gnugrep
  pkgs.gnused
  pkgs.gawk
  pkgs.jq
]
