## runpodctl network-volume update

update a network volume

### Synopsis

update an existing network volume

```
runpodctl network-volume update <volume-id> [flags]
```

### Options

```
  -h, --help          help for update
      --name string   new volume name
      --size int      new volume size in gb (must be larger than current)
```

### Options inherited from parent commands

```
      --insecure        allow a non-loopback http:// api url, which sends the api key unencrypted (or set RUNPOD_ALLOW_INSECURE_API=1)
  -o, --output string   output format (json, yaml) (default "json")
```

### SEE ALSO

* [runpodctl network-volume](runpodctl_network-volume.md)	 - manage network volumes

