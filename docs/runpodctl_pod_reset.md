## runpodctl pod reset

reset is not supported by api v2

### Synopsis

api v2 has no reset action. use `runpodctl pod restart <pod-id>` to restart a pod.

```
runpodctl pod reset <pod-id> [flags]
```

### Options

```
  -h, --help   help for reset
```

### Options inherited from parent commands

```
  -o, --output string   output format (json, yaml) (default "json")
```

### SEE ALSO

* [runpodctl pod](runpodctl_pod.md)	 - manage gpu pods
