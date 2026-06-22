## runpodctl serverless create

create a new endpoint

### Synopsis

create a new serverless endpoint.

requires either --template-id or --hub-id.
--hub-id accepts both SERVERLESS and POD hub listings.

examples:
  # create from a template
  runpodctl serverless create --template-id <id> --gpu-id "NVIDIA GeForce RTX 4090"

  # create from a template and attach a model
  runpodctl serverless create --template-id <id> --gpu-id "NVIDIA GeForce RTX 4090" --model-reference https://huggingface.co/Qwen/Qwen2.5-0.5B-Instruct:main

  # create a cpu endpoint
  runpodctl serverless create --template-id <id> --compute-type CPU

  # create from a hub repo
  runpodctl hub search vllm                         # find the hub id
  runpodctl serverless create --hub-id <id> --gpu-id "NVIDIA GeForce RTX 4090"

  # create from a hub repo and attach a model
  runpodctl serverless create --hub-id <id> --gpu-id "NVIDIA GeForce RTX 4090" --model-reference https://huggingface.co/Qwen/Qwen2.5-0.5B-Instruct:main

  # override or add env vars (hub defaults are included automatically)
  runpodctl serverless create --hub-id <id> --env MODEL_NAME=my-model --env MAX_TOKENS=4096

```
runpodctl serverless create [flags]
```

### Options

```
      --compute-type string           compute type (GPU or CPU) (default "GPU")
      --data-center-ids string        comma-separated list of data center ids
      --env strings                   env vars in KEY=VALUE format; overrides hub defaults (repeatable)
      --execution-timeout int         max seconds per request (default -1)
      --flash-boot                    enable flash boot (default true)
      --gpu-count int                 number of gpus per worker (default 1)
      --gpu-id string                 gpu id (from 'runpodctl gpu list')
  -h, --help                          help for create
      --hub-id string                 hub listing id; accepts both SERVERLESS and POD types (alternative to --template-id)
      --idle-timeout int              seconds before idle worker scales down (1-3600) (default -1)
      --instance-id string            cpu instance id for --compute-type CPU (e.g. cpu3g-4-16)
      --min-cuda-version string       minimum cuda version (e.g., 12.6)
      --model-reference stringArray   hugging face model url with a ref to cache on the endpoint, e.g. https://huggingface.co/<org>/<model>:main; works with --template-id or --hub-id, gpu only (repeatable)
      --name string                   endpoint name
      --network-volume-id string      network volume id to attach
      --network-volume-ids string     comma-separated network volume ids for multi-region
      --scale-by string               autoscale strategy: delay (seconds of queue wait) or requests (pending request count)
      --scale-threshold int           trigger point for autoscaler (delay: seconds, requests: count) (default -1)
      --template-id string            template id (required if no --hub-id)
      --workers-max int               maximum number of workers (default 3)
      --workers-min int               minimum number of workers
```

### Options inherited from parent commands

```
  -o, --output string   output format (json, yaml) (default "json")
```

### SEE ALSO

* [runpodctl serverless](runpodctl_serverless.md)	 - manage serverless endpoints

