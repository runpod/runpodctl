## runpodctl template create

create a new template

### Synopsis

create a new template

```
runpodctl template create [flags]
```

### Examples

```
  runpodctl template create --name private-gpu --image registry.example.com/team/image:tag --registry-auth-id <registry-auth-id>
  runpodctl template create --name dev --image example/image:tag --ports "22/tcp,8888/http" --port-labels "22=ssh,8888=jupyter lab"
```

### Options

```
      --container-disk-in-gb int   container disk size in gb (default 20)
      --docker-entrypoint string   comma-separated docker entrypoint commands
      --docker-start-cmd string    comma-separated docker start commands
      --env string                 environment variables as json object
  -h, --help                       help for create
      --image string               docker image name (required)
      --name string                template name (required)
      --port-labels string         port labels as comma-separated port=name pairs, or json when a name contains a comma (requires --ports)
      --ports string               comma-separated list of ports
      --readme string              readme content
      --registry-auth-id string    container registry auth id (from 'runpodctl registry list')
      --serverless                 is this a serverless template
      --volume-in-gb int           volume size in gb
      --volume-mount-path string   volume mount path (default "/workspace")
```

### Options inherited from parent commands

```
  -o, --output string   output format (json, yaml) (default "json")
```

### SEE ALSO

* [runpodctl template](runpodctl_template.md)	 - manage templates

