# Workload traces

A ZIP archive containing the generated workload traces will be
available separately soon. The trace data is intentionally not stored in this
Git repository.

Use `poisson.js` to replay a downloaded schedule:

```bash
TARGET_URL="http://service.example/path" \
SCHEDULE_FILE="./flattened_schedule.json" \
VUS=3000 \
k6 run --out csv=results.csv traces/poisson.js
```

`TARGET_URL` is required. `SCHEDULE_FILE`, `VUS`, `MAX_DURATION`, and
`REQUEST_TIMEOUT` are optional.
