## runpodctl pod get

get pod details

### Synopsis

get details for a specific pod by id.

runtimeStatus reports what the pod is actually doing, which desiredStatus
cannot: running (container up), initializing (placed, container not up yet -
image pull, create or boot), stopped, terminated, or unknown.
runtimeStatusReason carries a stable token when there is more to say.

```
runpodctl pod get <pod-id> [flags]
```

### Options

```
  -h, --help                     help for get
      --include-machine          include machine info
      --include-network-volume   include network volume info
```

### Options inherited from parent commands

```
  -o, --output string   output format (json, yaml) (default "json")
```

### SEE ALSO

* [runpodctl pod](runpodctl_pod.md)	 - manage gpu pods

