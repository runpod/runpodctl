## runpodctl serverless logs

read a serverless endpoint's worker logs

### Synopsis

read the logs of the workers backing a serverless endpoint.

output is json lines: one {source,line,ts,workerId} object per line, so it can be
piped straight into jq or read by an agent.

logs belong to a worker, not to the endpoint, so without --worker this resolves
the endpoint's workers and reads all of them at once, tagging each line with its
workerId. pass --worker to read one.

a crash-looping worker is the case this exists for: repeated system "start
container" lines with no container output means the container exits before the
handler runs, which leaves jobs sitting in the queue even though nothing is wrong
with capacity.

without --follow this replays recent lines and exits as soon as they stop
arriving. with --follow it keeps streaming, reconnecting on its own if the
connection drops. the worker set is resolved once at start, so a worker that
appears later needs the command re-run.

```
runpodctl serverless logs <endpoint-id> [flags]
```

### Examples

```
  # every worker's recent logs
  runpodctl serverless logs abc123

  # follow one worker
  runpodctl serverless logs abc123 --worker xyz789 --follow

  # the last hour, platform lines only, to see why a worker will not start
  runpodctl serverless logs abc123 --since 1h --source system
```

### Options

```
  -f, --follow              keep streaming new lines until interrupted
  -h, --help                help for logs
      --max-wait duration   how long to wait for output before exiting when not following (exits earlier once the replayed lines stop arriving) (default 5s)
      --since string        only logs after this point: a duration like 30m, 2h, 7d, or an rfc3339 timestamp. overrides --tail
      --source string       which log source to read: container, system, or both (default "both")
      --tail int            historical lines to replay before live output (0-5000; 0 = live only) (default 100)
      --worker string       read one worker by id instead of every worker on the endpoint
```

### Options inherited from parent commands

```
  -o, --output string   output format (json, yaml) (default "json")
```

### SEE ALSO

* [runpodctl serverless](runpodctl_serverless.md)	 - manage serverless endpoints

