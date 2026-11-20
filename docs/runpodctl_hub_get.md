## runpodctl hub get

get hub repo details

### Synopsis

get details for a hub repo by id or owner/name.

examples:
  runpodctl hub get clma1kziv00064iog9u6acj6z     # by listing id
  runpodctl hub get runpod/worker-vllm              # by owner/name

```
runpodctl hub get <id-or-owner/name> [flags]
```

### Options

```
  -h, --help   help for get
```

### Options inherited from parent commands

```
      --insecure        allow a non-loopback http:// api url, which sends the api key unencrypted (or set RUNPOD_ALLOW_INSECURE_API=1)
  -o, --output string   output format (json, yaml) (default "json")
```

### SEE ALSO

* [runpodctl hub](runpodctl_hub.md)	 - browse the runpod hub

