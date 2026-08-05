# Request-log analysis

This pipeline copies completed SLO Director logs from the EFS debug pod and
produces per-second request and latency statistics. 

## Prerequisites

- Python 3.10 or newer
- `kubectl` access to the cluster for fetching EFS logs

## Install the Python dependency

From the repository root:

```bash
python3 -m venv .venv
source .venv/bin/activate
python3 -m pip install -r experiments/analysis/request-logs/requirements.txt
```

## Fetch and analyze an experiment

The `ubuntu` debug pod and `efs-claim` must already be deployed. After the
experiment has stopped writing logs, run:

```bash
./scripts/fetch-request-logs.sh ratings
```

The optional second argument selects an explicit output directory:

```bash
./scripts/fetch-request-logs.sh ratings artifacts/my-ratings-run
```

By default, results are written to
`artifacts/request-logs/<deployment>/<UTC-run-id>/`.

| Variable | Default | Purpose |
| --- | --- | --- |
| `NAMESPACE` | `default` | Namespace containing the debug pod. |
| `DEBUG_POD` | `ubuntu` | Pod mounting `efs-claim`. |
| `DEBUG_CONTAINER` | `ubuntu` | Container used by `kubectl cp`. |
| `WINDOW_SECONDS` | `1` | Rolling window for latency percentiles. |
| `LATENCY_FIELD` | `request` | Analyze backend `request` duration or end-to-end `total` duration. |
| `KUBECTL` | `kubectl` | kubectl executable. |
| `PYTHON` | `python3` | Python executable. |

The output directory contains:

- `raw/`: copied `request_logs_*.txt` files;
- `aggregated_results.csv`: relative one-second latency buckets;
- `statistics.csv`: per-second request counts and rolling latency statistics;
- `vm-vs-serverless-requests.png`: headless VM/serverless request plot;
- `request-counts.txt`: per-file and total validated request counts.

`active_proxies` is the number of log files contributing at least one request
in a bucket. It is not the Kubernetes replica count.

The parser accepts every unit emitted by Go's `time.Duration.String`, including
`ns`, `µs`, `ms`, `s`, minutes, and hours. Malformed lines stop the analysis
instead of being silently discarded.

## Analyze logs already stored locally

```bash
ANALYSIS_DIR=experiments/analysis/request-logs
INPUT_DIR=/path/to/request-logs
OUTPUT_DIR=artifacts/local-analysis
mkdir -p "$OUTPUT_DIR"

python3 "$ANALYSIS_DIR/aggregate_request_logs.py" \
  --input-dir "$INPUT_DIR" \
  --output "$OUTPUT_DIR/aggregated_results.csv"
python3 "$ANALYSIS_DIR/generate_statistics.py" \
  --input "$OUTPUT_DIR/aggregated_results.csv" \
  --output "$OUTPUT_DIR/statistics.csv" \
  --window-seconds 1
python3 "$ANALYSIS_DIR/plot_requests.py" \
  --input "$OUTPUT_DIR/statistics.csv" \
  --output "$OUTPUT_DIR/vm-vs-serverless-requests.png"
python3 "$ANALYSIS_DIR/count_requests.py" \
  --input-dir "$INPUT_DIR"
```
