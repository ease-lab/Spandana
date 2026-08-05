# Running SRO

The launcher starts one shared resource manager, one scaling monitor, and one
SRO per supplied config. SLO Directors push one-millisecond request counts
directly to the SRO assigned to their deployment.

## Prerequisites

- Go 1.24.1
- tmux
- kubectl access to the target cluster
- connectivity from application pods to the SRO host

## Configure endpoints

Edit `deploy/kubernetes/apps/common/sro-endpoints.yaml` and replace
`sro-host.example` with an address reachable from the cluster.

| Service | Config | Port |
| --- | --- | ---: |
| ratings | `configs/sro/ratings.json` | 8087 |
| productpage | `configs/sro/productpage.json` | 8088 |
| details | `configs/sro/details.json` | 8089 |
| reviews | `configs/sro/reviews.json` | 8090 |
| compression | `configs/sro/compression.json` | 8091 |
| imgproc | `configs/sro/imgproc.json` | 8092 |

Apply the common resources and restart an existing deployment after changing
the ConfigMap:

```bash
kubectl apply -f deploy/kubernetes/apps/common/
kubectl rollout restart deployment/ratings
```

Application manifests populate `SRO_INGEST_URL` from the matching ConfigMap
key. Payloads also contain `DEPLOYMENT_NAME`, and an SRO rejects metrics for a
different deployment.

See [Deploying Spandana applications](deploying-apps.md) for the complete
Secret, `configMapKeyRef`, and EFS logging setup.

## Run one service

```bash
./scripts/start-sro.sh configs/sro/ratings.json
```

With no arguments, the launcher defaults to `ratings.json`.

## Run a service chain

```bash
./scripts/start-sro.sh \
  configs/sro/productpage.json \
  configs/sro/details.json \
  configs/sro/reviews.json \
  configs/sro/ratings.json
```

This creates `resource-manager` and `scaling-monitor` windows plus one
`sro-<service>` window per config. Each SRO listens on its configured address
and uses the resource manager on port 9000.

## Launcher environment variables

| Variable | Default | Purpose |
| --- | --- | --- |
| `TMUX_SESSION` | `spandana` | tmux session name. |
| `KUBECONFIG` | Kubernetes default | Resource-manager kubeconfig. |
| `SPANDANA_OUTPUT_ROOT` | `artifacts/sro` | Parent output directory. |
| `SPANDANA_RESOURCE_LOG` | `artifacts/resource-manager/cpu_metrics.log` | Resource-manager log. |
| `INTERVAL` | `15` | Scaling-monitor interval in seconds. |

When running `cmd/sro` directly, `SPANDANA_OUTPUT_DIR` selects its output
folder. The launcher sets it to
`<SPANDANA_OUTPUT_ROOT>/<configuration-name>`.

## SLO Director variables

| Variable | Default | Purpose |
| --- | --- | --- |
| `DEPLOYMENT_NAME` | Required | Identifies the metric stream. |
| `SRO_INGEST_URL` | Required | Full `/metrics/ingest` URL for that deployment. |
| `SERVERLESS_URL` | Required | Serverless fallback URL. Kubernetes manifests read it from the `spandana-serverless-backends` Secret. |
| `VM_BACKEND_URL` | `http://localhost` | VM-backed application address. |
| `VM_BACKEND_PORT` | `5000` | VM-backed application port. |
| `PROXY_PORT` | `8000` | SLO Director listen port. |
| `RATE_LIMIT` | `350` | Initial VM request rate in requests per second. |
| `QUEUE_SIZE` | `1` | Number of requests allowed to wait for the VM backend. |
| `LATENCY_SLO_MS` | `100` | Latency SLO in milliseconds. |
| `SLO_PASS_THRESHOLD` | `100` | Consecutive passing intervals before an adjustment. |
| `SLO_VIOLATION_THRESHOLD` | `100` | Consecutive violating intervals before an adjustment. |
| `POD_NAME` | `unknown-pod` | Name included in local log filenames. |

Before deploying an application, create the `spandana-serverless-backends`
Secret in the same namespace. It may contain any subset of these keys matching
the applications being deployed: `ratings-url`, `details-url`,
`compression-url`, and `imgproc-url`. The endpoint values are intentionally not
included in this repository.

The SLO Director sends a ten-second batch asynchronously. Health and readiness
probes are not counted.

## Tmux

```bash
tmux attach-session -t spandana
tmux kill-session -t spandana
```
