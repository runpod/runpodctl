## runpodctl billing pods

view pod billing history

### Synopsis

view billing history for pods, one record per pod per time bucket, split into gpu, cpu and disk cost

```
runpodctl billing pods [flags]
```

### Options

```
      --bucket-size string   bucket size (hour, day, week, month, year) (default "day")
      --end-time string      end time (RFC3339 format)
  -h, --help                 help for pods
      --pod-id string        filter by pod id
      --start-time string    start time (RFC3339 format)
```

### Options inherited from parent commands

```
  -o, --output string   output format (json, yaml) (default "json")
```

### SEE ALSO

* [runpodctl billing](runpodctl_billing.md)	 - view billing history

