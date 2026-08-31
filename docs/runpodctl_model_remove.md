## runpodctl model remove

remove a model

### Synopsis

remove a model from the runpod model repository. blocked if any endpoints still reference it — a model/version in active use cannot be removed.

```
runpodctl model remove [flags]
```

### Examples

```
  # blocked if an endpoint still references this model, listing which one(s)
  runpodctl model remove --owner <owner> --name my-model

  # detach it from the endpoint first, then remove
  runpodctl serverless update <endpoint-id> --clear-models
  runpodctl model remove --owner <owner> --name my-model
```

### Options

```
      --hash string      model version hash to remove
  -h, --help             help for remove
      --name string      model name
      --owner string     model owner
      --version string   model version uuid to remove
```

### Options inherited from parent commands

```
  -o, --output string   output format (json, yaml) (default "json")
```

### SEE ALSO

* [runpodctl model](runpodctl_model.md)	 - manage model repository
