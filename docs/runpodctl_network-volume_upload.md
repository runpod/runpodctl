## runpodctl network-volume upload

upload a file or folder to a network volume

### Synopsis

upload a file or folder to a network volume.

remote-path defaults to the local name. a file uploaded to a remote path ending
in "/" keeps its name inside that folder; a folder's contents land under
remote-path. files whose remote copy has the same size and is not older are
skipped.

uses the network volume's s3-compatible api, which needs s3 api keys (separate
from your runpod api key): create them at https://console.runpod.io/user/settings
and set RUNPOD_S3_ACCESS_KEY and RUNPOD_S3_SECRET_KEY, pass the flags, or set
s3AccessKey and s3SecretKey in ~/.runpod/config.toml.

the volume's data center is looked up with your runpod api key; pass
--data-center-id to skip the lookup. only data centers with the s3 api are
supported: https://docs.runpod.io/storage/s3-api

transfers resume: if one stops (network drop, ctrl-c, crash), run the same
command again. finished files are skipped, a large upload continues from its
uploaded parts, and a large download continues from its finished chunks.
--force sends every file again, for same-size edits the time check misses.

```
runpodctl network-volume upload <volume-id> <local-path> [remote-path] [flags]
```

### Examples

```
  runpodctl network-volume upload abc123 ./model.safetensors models/
  runpodctl network-volume upload abc123 ./dataset datasets/dataset
```

### Options

```
      --concurrency int         parallel requests (1-64) (default 4)
      --data-center-id string   volume data center id, e.g. EU-RO-1 (default: looked up from the volume)
      --force                   transfer every file, even ones the size and time check calls unchanged
  -h, --help                    help for upload
      --s3-access-key string    s3 api access key (default: $RUNPOD_S3_ACCESS_KEY)
      --s3-secret-key string    s3 api secret key (default: $RUNPOD_S3_SECRET_KEY)
```

### Options inherited from parent commands

```
  -o, --output string   output format (json, yaml) (default "json")
```

### SEE ALSO

* [runpodctl network-volume](runpodctl_network-volume.md)	 - manage network volumes

