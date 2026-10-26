## runpodctl network-volume download

download a file or folder from a network volume

### Synopsis

download a file or folder from a network volume.

remote-path is a folder when it ends in "/" or has files under it. local-path
defaults to the remote name; a file downloaded into an existing local folder
keeps its name. local files with the remote size are skipped. use "/" as
remote-path to download the whole volume.

uses the network volume's s3-compatible api, which needs s3 api keys (separate
from your runpod api key): create them at https://console.runpod.io/user/settings
and set RUNPOD_S3_ACCESS_KEY and RUNPOD_S3_SECRET_KEY, or pass the flags.

the volume's data center is looked up with your runpod api key; pass
--data-center-id to skip the lookup. only data centers with the s3 api are
supported: https://docs.runpod.io/storage/s3-api

transfers resume: if one stops (network drop, ctrl-c, crash), run the same
command again. finished files are skipped, a large upload continues from its
uploaded parts, and a large download continues from its finished chunks.

```
runpodctl network-volume download <volume-id> <remote-path> [local-path] [flags]
```

### Examples

```
  runpodctl network-volume download abc123 models/model.safetensors
  runpodctl network-volume download abc123 outputs/ ./outputs
```

### Options

```
      --concurrency int         parallel requests (1-64) (default 4)
      --data-center-id string   volume data center id, e.g. EU-RO-1 (default: looked up from the volume)
  -h, --help                    help for download
      --s3-access-key string    s3 api access key (default: $RUNPOD_S3_ACCESS_KEY)
      --s3-secret-key string    s3 api secret key (default: $RUNPOD_S3_SECRET_KEY)
```

### Options inherited from parent commands

```
  -o, --output string   output format (json, yaml) (default "json")
```

### SEE ALSO

* [runpodctl network-volume](runpodctl_network-volume.md)	 - manage network volumes

