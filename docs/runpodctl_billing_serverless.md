## runpodctl billing serverless

view serverless billing history

### Synopsis

view billing history for serverless endpoints, one record per endpoint per time bucket, split into gpu, cpu, disk and fee cost

```
runpodctl billing serverless [flags]
```

### Options

```
      --bucket-size string   bucket size (hour, day, week, month, year) (default "day")
      --end-time string      end time (RFC3339 format)
      --endpoint-id string   filter by endpoint id
  -h, --help                 help for serverless
      --start-time string    start time (RFC3339 format)
```

### Options inherited from parent commands

```
  -o, --output string   output format (json, yaml) (default "json")
```

### SEE ALSO

* [runpodctl billing](runpodctl_billing.md)	 - view billing history

