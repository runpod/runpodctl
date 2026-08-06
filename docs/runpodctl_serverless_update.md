## runpodctl serverless update

update an endpoint

### Synopsis

update an existing serverless endpoint.

examples:
  # rename an endpoint
  runpodctl serverless update <id> --name my-endpoint

  # set model references (replaces existing)
  runpodctl serverless update <id> --model-reference https://huggingface.co/Qwen/Qwen2.5-0.5B-Instruct:main

  # attach multiple models
  runpodctl serverless update <id> --model-reference <ref-a> --model-reference <ref-b>

  # clear all model references
  runpodctl serverless update <id> --clear-models

```
runpodctl serverless update <endpoint-id> [flags]
```

### Options

```
      --clear-models                  remove all model references from the endpoint
  -h, --help                          help for update
      --idle-timeout int              new idle timeout in seconds (1-3600) (default -1)
      --model-reference stringArray   model reference to cache on the endpoint (repeatable); replaces existing model references
      --name string                   new endpoint name
      --scale-by string               autoscale strategy: delay (seconds of queue wait) or requests (pending request count)
      --scale-threshold int           trigger point for autoscaler (delay: seconds, requests: count) (default -1)
      --template-id string            new template id
      --workers-max int               new maximum number of workers (default -1)
      --workers-min int               new minimum number of workers (default -1)
```

### Options inherited from parent commands

```
  -o, --output string   output format (json, yaml) (default "json")
```

### SEE ALSO

* [runpodctl serverless](runpodctl_serverless.md)	 - manage serverless endpoints
