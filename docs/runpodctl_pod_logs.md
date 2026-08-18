## runpodctl pod logs

read a pod's logs

### Synopsis

read a pod's container and system logs.

output is json lines: one {source,line,ts} object per line, so it can be piped
straight into jq or read by an agent.

source is either container (your workload's own output) or system (the platform
narrating image pull, container create and start). a stalled deploy usually shows
up in the system lines: repeated pull progress, or a create that never reaches
start.

without --follow this replays recent lines and exits as soon as they stop
arriving. with --follow it keeps streaming, reconnecting on its own if the
connection drops.

```
runpodctl pod logs <pod-id> [flags]
```

### Examples

```
  # last 100 lines, then exit
  runpodctl pod logs abc123

  # follow live output
  runpodctl pod logs abc123 --follow

  # the last 30 minutes of platform lines only
  runpodctl pod logs abc123 --since 30m --source system

  # live output only, no history
  runpodctl pod logs abc123 --tail 0 --follow
```

### Options

```
  -f, --follow              keep streaming new lines until interrupted
  -h, --help                help for logs
      --max-wait duration   how long to wait for output before exiting when not following (exits earlier once the replayed lines stop arriving) (default 5s)
      --since string        only logs after this point: a duration like 30m, 2h, 7d, or an rfc3339 timestamp. overrides --tail
      --source string       which log source to read: container, system, or both (default "both")
      --tail int            historical lines to replay before live output (0-5000; 0 = live only) (default 100)
```

### Options inherited from parent commands

```
  -o, --output string   output format (json, yaml) (default "json")
```

### SEE ALSO

* [runpodctl pod](runpodctl_pod.md)	 - manage gpu pods

