## runpodctl serverless list

list all endpoints

### Synopsis

list all serverless endpoints in your account.

idleTimeout is null when not applicable, including queue-based endpoints
using request-count scaling.

```
runpodctl serverless list [flags]
```

### Options

```
  -h, --help               help for list
      --include-template   include the container config (image, disk, args, env, ports); env values may contain secrets
      --include-workers    include workers info
```

### Options inherited from parent commands

```
  -o, --output string   output format (json, yaml) (default "json")
```

### SEE ALSO

* [runpodctl serverless](runpodctl_serverless.md)	 - manage serverless endpoints
