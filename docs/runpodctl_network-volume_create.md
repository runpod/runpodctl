## runpodctl network-volume create

create a new network volume

### Synopsis

create a new network volume

```
runpodctl network-volume create [flags]
```

### Options

```
      --data-center-id string   data center id (required)
  -h, --help                    help for create
      --name string             volume name (required)
      --size int                volume size in gb (10-4096, required)
```

### Options inherited from parent commands

```
      --insecure        allow a non-loopback http:// api url, which sends the api key unencrypted (or set RUNPOD_ALLOW_INSECURE_API=1)
  -o, --output string   output format (json, yaml) (default "json")
```

### SEE ALSO

* [runpodctl network-volume](runpodctl_network-volume.md)	 - manage network volumes

