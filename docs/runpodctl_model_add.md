## runpodctl model add

add a model

### Synopsis

add a model to the runpod model repository

```
runpodctl model add [flags]
```

### Options

```
      --content-type string           upload content type
      --create-upload                 create an upload session
      --credential-reference string   credential reference (if required)
      --credential-type string        credential type (if required)
      --file-name string              file name for upload
      --file-size string              file size in bytes
      --hash-timeout duration         maximum duration to wait for --wait-for-hash (0 disables timeout) (default 30m0s)
  -h, --help                          help for add
      --metadata stringToString       metadata key=value pairs (default [])
      --model-path string             directory containing model files to upload
      --model-status string           initial model status
      --name string                   model name
      --owner string                  model owner namespace (user or team owner id)
      --part-size string              multipart upload part size in bytes
  -v, --verbose                       include upload details in wait-for-hash output
      --wait-for-hash                 wait for completed model-path uploads to be hashed
```

### Options inherited from parent commands

```
  -o, --output string   output format (json, yaml) (default "json")
```

### SEE ALSO

* [runpodctl model](runpodctl_model.md)	 - manage model repository
