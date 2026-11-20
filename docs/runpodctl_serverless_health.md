## runpodctl serverless health

get endpoint health

### Synopsis

get the health of a serverless endpoint: worker counts by state and job counts by outcome.

the response comes from the invoke api verbatim, so new fields appear without a cli update.

examples:
  runpodctl serverless health <endpoint-id>

```
runpodctl serverless health <endpoint-id> [flags]
```

### Options

```
  -h, --help   help for health
```

### Options inherited from parent commands

```
      --insecure        allow a non-loopback http:// api url, which sends the api key unencrypted (or set RUNPOD_ALLOW_INSECURE_API=1)
  -o, --output string   output format (json, yaml) (default "json")
```

### SEE ALSO

* [runpodctl serverless](runpodctl_serverless.md)	 - manage serverless endpoints

