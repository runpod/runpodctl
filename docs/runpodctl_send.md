## runpodctl send

send files or folders

### Synopsis

send files or folders to a pod or any computer using croc.

send prints a one-time code ending in a relay index; run 'runpodctl receive <code>'
on the other machine (e.g. inside a pod) with that exact code to complete the
transfer.

with --code, the code is not one-time: it is your codephrase plus the relay index
that send prints (e.g. my-secret-14), and the receiver must type the printed value.

```
runpodctl send <file> [flags]
```

### Examples

```
  # on this machine: prints a one-time code like 5254-respect-amen-emotion-14
  runpodctl send ./model.safetensors

  # then, on the pod (or any other computer), use the exact code send printed:
  runpodctl receive 5254-respect-amen-emotion-14

  # with a fixed codephrase (minimum 6 characters), send prints my-secret-<relay index>:
  runpodctl send --code my-secret ./model.safetensors
```

### Options

```
      --code string   codephrase used to connect
  -h, --help          help for send
```

### Options inherited from parent commands

```
  -o, --output string   output format (json, yaml) (default "json")
```

### SEE ALSO

* [runpodctl](runpodctl.md)	 - cli for runpod.io

