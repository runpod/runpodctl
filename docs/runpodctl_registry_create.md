## runpodctl registry create

create a new registry auth

### Synopsis

create a new container registry authentication

the password can come from --password, from stdin with --password-stdin, or from
an interactive prompt when neither flag is given. --password-stdin keeps the
credential out of the process table and your shell history.

```
runpodctl registry create [flags]
```

### Examples

```
  # read the password from a pipe
  echo "$REGISTRY_TOKEN" | runpodctl registry create --name ghcr --username me --password-stdin

  # read it from a file without exposing it to the process table
  runpodctl registry create --name ghcr --username me --password-stdin < token.txt

  # prompt for it, without echo
  runpodctl registry create --name ghcr --username me
```

### Options

```
  -h, --help              help for create
      --name string       registry auth name (required)
      --password string   registry password; see also --password-stdin
      --password-stdin    read the registry password from stdin
      --username string   registry username (required)
```

### Options inherited from parent commands

```
  -o, --output string   output format (json, yaml) (default "json")
```

### SEE ALSO

* [runpodctl registry](runpodctl_registry.md)	 - manage container registry auth

