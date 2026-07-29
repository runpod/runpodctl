## runpodctl pod list

list all pods

### Synopsis

list all pods in your account.

defaults to running pods only; use --all to include stopped ones.

runtimeStatus reports what each pod is actually doing, which desiredStatus
cannot: running (container up), initializing (placed, container not up yet -
image pull, create or boot), stopped, terminated, or unknown.
runtimeStatusReason carries a stable token when there is more to say.

```
runpodctl pod list [flags]
```

### Options

```
  -a, --all                    show all pods including exited (default: running only)
      --compute-type string    filter by compute type (GPU or CPU)
      --created-after string   filter pods created after date (e.g. 2025-01-15)
  -h, --help                   help for list
      --name string            filter by pod name
      --since string           filter pods created within duration (e.g. 1h, 7d)
      --status string          filter by desired status (e.g. RUNNING, EXITED)
```

### Options inherited from parent commands

```
  -o, --output string   output format (json, yaml) (default "json")
```

### SEE ALSO

* [runpodctl pod](runpodctl_pod.md)	 - manage gpu pods

