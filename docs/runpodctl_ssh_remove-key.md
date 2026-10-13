## runpodctl ssh remove-key

remove an ssh key

### Synopsis

remove an ssh key from your account

```
runpodctl ssh remove-key [flags]
```

### Options

```
      --fingerprint string   fingerprint of the key to remove
  -h, --help                 help for remove-key
      --invalid              remove every saved entry that isn't a valid public key (they block all key changes)
      --name string          name of the key to remove
```

### Options inherited from parent commands

```
  -o, --output string   output format (json, yaml) (default "json")
```

### SEE ALSO

* [runpodctl ssh](runpodctl_ssh.md)	 - manage ssh keys and connections

