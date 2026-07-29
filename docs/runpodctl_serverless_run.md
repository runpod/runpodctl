## runpodctl serverless run

invoke an endpoint and wait for the result

### Synopsis

invoke a serverless endpoint with a json payload and wait for the job to finish.

the payload must be a json object and is sent as {"input": <your json>}; pass
only the handler payload.

the job is submitted on /run and then polled on /status until it is terminal.
/runsync is deliberately not used: it only holds the connection for 90 seconds
and then hands back a still-running job, and until it answers there is no job id
to poll, so a slow response would leave a running job unreachable.

waiting is bounded by --wait (default 5m). when it runs out the job is still
running server-side: the last payload is printed on stdout, a "timeout" error on
stderr names the 'serverless status' command to poll it, and the exit code is 1.
a single api call inside the wait is never given less than 1s, so a --wait below
one second may overshoot by up to that much.

exit codes: 0 when the job is COMPLETED, and when --wait 0 / --no-wait submitted
it successfully. 1 when the request fails, when the wait budget runs out, or when
the job ends FAILED / CANCELLED / TIMED_OUT. the job payload (including the
worker's own error) is still printed on stdout in every one of those cases.

examples:
  # invoke and wait for the result
  runpodctl serverless run <endpoint-id> --input '{"prompt":"hello"}'

  # big payloads: skip shell quoting entirely
  runpodctl serverless run <endpoint-id> --input-file payload.json
  cat payload.json | runpodctl serverless run <endpoint-id> --input -

  # give a cold or slow endpoint longer
  runpodctl serverless run <endpoint-id> --input '{}' --wait 15m

  # submit and get the job id back immediately
  runpodctl serverless run <endpoint-id> --input '{}' --no-wait
  runpodctl serverless status <endpoint-id> <job-id>

```
runpodctl serverless run <endpoint-id> [flags]
```

### Options

```
  -h, --help                help for run
      --input string        json payload for the handler; '-' reads stdin
      --input-file string   read the json payload from a file; '-' reads stdin
      --no-wait             submit and print the job id without waiting (same as --wait 0)
      --wait duration       how long to wait for a terminal job status; 0 does not wait (e.g. 90s, 10m) (default 5m0s)
```

### Options inherited from parent commands

```
  -o, --output string   output format (json, yaml) (default "json")
```

### SEE ALSO

* [runpodctl serverless](runpodctl_serverless.md)	 - manage serverless endpoints

