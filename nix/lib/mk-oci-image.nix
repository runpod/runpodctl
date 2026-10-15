#
# nix/lib/mk-oci-image.nix — runpodctl
#
# `dockerTools.streamLayeredImage` wrapper shared by both images (design §5):
# no base image and no shell, non-root, the CLI as the entrypoint.
#
# The result is a script that streams the image tarball:
#
#   nix build .#oci-runpodctl && ./result | docker load
#
# Non-root needs a writable home: `initConfig` always creates
# ~/.runpod/config.toml, and the ssh paths write ~/.runpod/ssh/known_hosts.
# `fakeRootCommands` creates it (and /tmp) owned by the runtime uid, so the
# image works without `--user` or a volume. It also writes /etc/passwd and
# /etc/group: openssh refuses to start ("No user exists for uid 65532") when
# the uid has no entry.
#
{ pkgs, lib }:
{
  name,
  runpodctl,
  rev,
  extraContents ? [ ],
}:
let
  uid = "65532";
  user = "runpodctl";
  home = "/home/${user}";
in
pkgs.dockerTools.streamLayeredImage {
  inherit name;
  tag = "${runpodctl.version}-${rev}";

  contents = [
    runpodctl
    pkgs.dockerTools.caCertificates
  ]
  ++ extraContents;

  enableFakechroot = true;
  fakeRootCommands = ''
    mkdir -p ${home} /tmp /etc
    echo "${user}:x:${uid}:${uid}::${home}:/sbin/nologin" > /etc/passwd
    echo "${user}:x:${uid}:" > /etc/group
    chown ${uid}:${uid} ${home}
    chmod 0700 ${home}
    chmod 1777 /tmp
  '';

  config = {
    Entrypoint = [ (lib.getExe runpodctl) ];
    User = "${uid}:${uid}";
    WorkingDir = home;
    Env = [
      "HOME=${home}"
      "SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt"
    ];
    Labels = {
      "org.opencontainers.image.title" = name;
      "org.opencontainers.image.description" = runpodctl.meta.description;
      "org.opencontainers.image.source" = runpodctl.meta.homepage;
      "org.opencontainers.image.revision" = rev;
      "org.opencontainers.image.version" = "${runpodctl.version}-${rev}";
      "org.opencontainers.image.licenses" = "GPL-3.0-only";
    };
  };
}
