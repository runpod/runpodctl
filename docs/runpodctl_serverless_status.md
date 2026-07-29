## runpodctl serverless status

get the status of a serverless job

### Synopsis

get the status of a job previously submitted to an endpoint (/status/<job-id>).

this is the follow-up for 'serverless run --no-wait' and for a run that hit its
--wait budget. one check by default; pass --wait to keep polling until the job is
terminal.

exit codes: 0 when the job is COMPLETED or still queued/running, 1 when the job
ended FAILED / CANCELLED / TIMED_OUT or when --wait ran out. the job payload is
printed on stdout either way.

examples:
  runpodctl serverless status <endpoint-id> <job-id>
  runpodctl serverless status <endpoint-id> <job-id> --wait 5m

```
runpodctl serverless status <endpoint-id> <job-id> [flags]
```

### Options

```
  -h, --help            help for status
      --wait duration   keep polling until the job is terminal, up to this long (0 = check once)
```

### Options inherited from parent commands

```
  -o, --output string   output format (json, yaml) (default "json")
```

### SEE ALSO

* [runpodctl serverless](runpodctl_serverless.md)	 - manage serverless endpoints

