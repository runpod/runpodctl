#
# nix/containers/default.nix — runpodctl
#
# The two OCI images (design §5), linux only; each linux system builds its
# own architecture natively.
#
#   oci-runpodctl        the CLI + CA certificates: every API command, logs,
#                        send/receive.
#   oci-runpodctl-full   + rsync, openssh, coreutils: the `project` sync path,
#                        which runs `rsync -e ssh …`.
#
# The API key is passed at run time (`-e RUNPOD_API_KEY`); viper's
# AutomaticEnv reads it, so nothing secret is ever baked into a layer.
#
# The attrset is consumed whole (packages, the size report, oci-push), so a
# new image is picked up everywhere without another edit.
#
{
  pkgs,
  lib,
  runpodctl,
  rev,
}:
let
  mkOciImage = import ../lib/mk-oci-image.nix { inherit pkgs lib; };

in
{
  oci-runpodctl = mkOciImage {
    name = "runpodctl";
    inherit runpodctl rev;
  };

  oci-runpodctl-full = mkOciImage {
    name = "runpodctl-full";
    inherit runpodctl rev;
    extraContents = [
      pkgs.rsync
      pkgs.openssh
      pkgs.coreutils
    ];
  };
}
