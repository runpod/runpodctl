## runpodctl serverless env

show an endpoint's own live environment variable override

### Synopsis

show the environment variable override configured directly on a
serverless endpoint (GraphQL Endpoint.env). this is the override that
reaches the running container, not the template's default env that
`runpodctl template get` reports.

an empty result means the endpoint has no override and runs on its
template's default env.

the override can contain secrets (an HF_TOKEN, an api key) -- don't paste
its output verbatim into a ticket or chat.

```
runpodctl serverless env <endpoint-id> [flags]
```

### Examples

```
  # the env var override configured directly on an endpoint
  runpodctl serverless env abc123
```

### Options

```
  -h, --help   help for env
```

### Options inherited from parent commands

```
  -o, --output string   output format (json, yaml) (default "json")
```

### SEE ALSO

* [runpodctl serverless](runpodctl_serverless.md)	 - manage serverless endpoints

