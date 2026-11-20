## runpodctl serverless get

get endpoint details

### Synopsis

get details for a specific endpoint by id.

idleTimeout is null when not applicable, including queue-based endpoints
using request-count scaling.

```
runpodctl serverless get <endpoint-id> [flags]
```

### Options

```
  -h, --help               help for get
      --include-template   include the container config (image, disk, args, env, ports); env values may contain secrets
      --include-workers    include workers info
```

### Options inherited from parent commands

```
      --insecure        allow a non-loopback http:// api url, which sends the api key unencrypted (or set RUNPOD_ALLOW_INSECURE_API=1)
  -o, --output string   output format (json, yaml) (default "json")
```

### SEE ALSO

* [runpodctl serverless](runpodctl_serverless.md)	 - manage serverless endpoints
