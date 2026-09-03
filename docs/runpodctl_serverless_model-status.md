## runpodctl serverless model-status

diagnose model repo assignment and mount status for an endpoint's workers

### Synopsis

show a serverless endpoint's configured model references and, for each
worker, the resolved model version hash, machine assignment status, failure
phase and reason (if any), and expected mount path.

this exists to let you diagnose why a model repo-backed endpoint is stuck
loading or failing to mount a model, without ssh access or internal host logs.

a status of FAILED carries a failurePhase (download_failed, mount_failed,
startup_failed, or cuda_failed) and a failureReason summarizing what went
wrong. for the full free-text detail behind that summary, follow up with
`runpodctl serverless logs <endpoint-id> --source system`.

not covered here: download/mount progress percentage, and the container's
effective MODEL_NAME/MODEL_REVISION environment variables -- neither is
exposed by any api today.

```
runpodctl serverless model-status <endpoint-id> [flags]
```

### Examples

```
  # every worker's model assignment status for an endpoint
  runpodctl serverless model-status abc123
```

### Options

```
  -h, --help   help for model-status
```

### Options inherited from parent commands

```
  -o, --output string   output format (json, yaml) (default "json")
```

### SEE ALSO

* [runpodctl serverless](runpodctl_serverless.md)	 - manage serverless endpoints

