## runpodctl template update

update a template

### Synopsis

update an existing template

```
runpodctl template update <template-id> [flags]
```

### Examples

```
  runpodctl template update <template-id> --registry-auth-id <registry-auth-id>
  runpodctl template update <template-id> --port-labels "22=ssh,8888=jupyter lab"
```

### Options

```
      --container-disk-in-gb int   new container disk size in gb (default -1)
      --env string                 new environment variables as json object
  -h, --help                       help for update
      --image string               new docker image name
      --name string                new template name
      --port-labels string         new port labels as port=name pairs or json; pass an empty value to clear
      --ports string               new comma-separated list of ports
      --readme string              new readme content
      --registry-auth-id string    new container registry auth id; pass an empty value to clear
```

### Options inherited from parent commands

```
  -o, --output string   output format (json, yaml) (default "json")
```

### SEE ALSO

* [runpodctl template](runpodctl_template.md)	 - manage templates
