## runpodctl doctor

diagnose and fix cli issues

### Synopsis

check runpod connectivity and fix configuration issues

when no api key is configured, doctor prompts on stderr and reads one from
stdin. --api-key always validates the given key and saves it, replacing
any saved key. a key is only saved after the api accepts it, and a run that
sets up a key exits non-zero if doctor ends unhealthy.

```
runpodctl doctor [flags]
```

### Examples

```
  # interactive: prompts for the api key and saves it
  runpodctl doctor

  # non-interactive (ci, containers, agents): pipe the key on stdin, which
  # keeps it out of shell history and ps. read only when no key is configured
  printf %s "$MY_RUNPOD_KEY" | runpodctl doctor

  # or pass it as a flag, which also replaces an already saved key
  runpodctl doctor --api-key rpa_yourkey
```

### Options

```
      --api-key string   api key to validate and save, replacing any saved key (visible in shell history and ps; prefer stdin)
  -h, --help             help for doctor
```

### Options inherited from parent commands

```
  -o, --output string   output format (json, yaml) (default "json")
```

### SEE ALSO

* [runpodctl](runpodctl.md)	 - cli for runpod.io

