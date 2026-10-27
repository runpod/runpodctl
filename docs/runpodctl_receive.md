## runpodctl receive

receive files or folders

### Synopsis

receive files or folders from a pod or any computer using croc.

use the exact code printed by 'runpodctl send' on the sending machine, including
the trailing relay index. if the sender used --code, type the printed
<codephrase>-<relay index>, not the bare codephrase.

```
runpodctl receive <code> [flags]
```

### Examples

```
  # receive using the exact code printed by 'runpodctl send'
  runpodctl receive 5254-respect-amen-emotion-14

  # sender used --code my-long-secret-phrase and send printed my-long-secret-phrase-14
  runpodctl receive my-long-secret-phrase-14
```

### Options

```
  -h, --help   help for receive
```

### Options inherited from parent commands

```
  -o, --output string   output format (json, yaml) (default "json")
```

### SEE ALSO

* [runpodctl](runpodctl.md)	 - cli for runpod.io

