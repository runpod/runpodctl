## runpodctl ssh info

show ssh info for a pod

### Synopsis

show ssh info for a pod (command + key). does not connect.

```
runpodctl ssh info <pod-id> [flags]
```

### Options

```
  -h, --help      help for info
  -v, --verbose   include pod id and name in output
```

### Options inherited from parent commands

```
      --insecure        allow a non-loopback http:// api url, which sends the api key unencrypted (or set RUNPOD_ALLOW_INSECURE_API=1)
  -o, --output string   output format (json, yaml) (default "json")
```

### SEE ALSO

* [runpodctl ssh](runpodctl_ssh.md)	 - manage ssh keys and connections

