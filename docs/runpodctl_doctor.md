## runpodctl doctor

diagnose and fix cli issues

### Synopsis

check runpod connectivity and fix configuration issues

```
runpodctl doctor [flags]
```

### Examples

```
  # interactive: prompts for the api key and saves it
  runpodctl doctor

  # non-interactive (ci, containers, agents): pass the key
  runpodctl doctor --api-key "$RUNPOD_API_KEY"
```

### Options

```
      --api-key string   api key to save (skips the interactive prompt)
  -h, --help             help for doctor
```

### Options inherited from parent commands

```
  -o, --output string   output format (json, yaml) (default "json")
```

### SEE ALSO

* [runpodctl](runpodctl.md)	 - cli for runpod.io

