## runpodctl serverless run

invoke an endpoint and wait for the result

### Synopsis

invoke a serverless endpoint with a json payload and wait for the job to finish.

the payload is sent as {"input": <your json>}; pass only the handler payload.

by default the job goes to /runsync. the invoke api stops holding that connection
after about 90 seconds, so a longer job is picked up on /status automatically —
--async only changes the submit route, not whether the cli waits.

waiting is bounded by --wait (default 5m). when it runs out the job is still
running server-side: the last payload is printed on stdout, a "timeout" error on
stderr names the 'serverless status' command to poll it, and the exit code is 1.

exit codes: 0 when the job is COMPLETED, 1 when the request fails, when the wait
budget runs out, or when the job ends FAILED / CANCELLED / TIMED_OUT. the job
payload (including the worker's own error) is still printed on stdout in every
one of those cases.

examples:
  # invoke and wait for the result
  runpodctl serverless run <endpoint-id> --input '{"prompt":"hello"}'

  # big payloads: skip shell quoting entirely
  runpodctl serverless run <endpoint-id> --input-file payload.json
  cat payload.json | runpodctl serverless run <endpoint-id> --input -

  # queue on /run and poll until it finishes
  runpodctl serverless run <endpoint-id> --input '{}' --async

  # queue on /run and get the job id back immediately
  runpodctl serverless run <endpoint-id> --input '{}' --no-wait
  runpodctl serverless status <endpoint-id> <job-id>

```
runpodctl serverless run <endpoint-id> [flags]
```

### Options

```
      --async               submit on /run instead of /runsync, then poll /status until the job is terminal
  -h, --help                help for run
      --input string        json payload for the handler; '-' reads stdin
      --input-file string   read the json payload from a file; '-' reads stdin
      --no-wait             submit on /run and print the job id without waiting (implies --async)
      --wait duration       how long to wait for a terminal job status (e.g. 90s, 10m) (default 5m0s)
```

### Options inherited from parent commands

```
  -o, --output string   output format (json, yaml) (default "json")
```

### SEE ALSO

* [runpodctl serverless](runpodctl_serverless.md)	 - manage serverless endpoints

