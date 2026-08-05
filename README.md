# Spandana

Spandana separates strict latency SLO enforcement from cost optimization. A
local SLO Director steers requests between VM-backed application instances and
a serverless backend, while a global SRO adjusts the number of application
pods.

This repository contains the artifact for **“Spandana: Reconciling Strict SLOs
with Low Cost under Fine-Grained Load Fluctuations,”** accepted at ACM SoCC
2026. Read the [preprint](paper/Spandana-preprint.pdf) or
[arXiv:2606.30533](https://arxiv.org/abs/2606.30533).

## Architecture

Spandana has three runtime programs:

- **SLO Director** (`cmd/slo-director`) enforces the latency SLO, redirects
  overflow requests to serverless, records arrivals at one-millisecond
  resolution, and pushes ten-second batches directly to its SRO.
- **SRO** (`cmd/sro`) receives those metrics, simulates the bounded queue at
  one-millisecond resolution, evaluates VM and serverless cost, and selects a
  pod count.
- **Resource manager** (`cmd/resource-manager`) reads and updates Kubernetes
  replicas through a local endpoint on port 9000.

In the new implementation, we removed Loki, Fluent Bit, and the intermediate telemetry fetcher to reduce system overhead and improve the accuracy of collected metrics. Multiple SRO instances share one resource manager and use separate ingest ports.

## Repository layout

| Path | Description |
| --- | --- |
| `cmd/` | Go entry points for the three runtime components. |
| `internal/costcalc/` | Shared VM and serverless cost calculations. |
| `configs/sro/` | Per-service SRO parameters and ingest addresses. |
| `deploy/kubernetes/` | Application manifests and SRO-related RBAC. |
| `scripts/` | Launching, monitoring, and cleanup helpers. |
| `tools/` | Optional AWS load-balancer setup helper. |
| `experiments/analysis/` | Experiment analysis and plotting scripts. |
| `traces/` | k6 replay script and external trace information. |
| `docs/` | Detailed run instructions and API notes. |
| `paper/` | Spandana preprint. |

## Prerequisites

- Go 1.24.1
- a Kubernetes cluster and `kubectl`
- `tmux`
- Python 3 for analysis
- k6 for workload replay
- Helm only for the optional AWS controller installer

## Build and test

```bash
go test ./...
go build ./cmd/...
```

Build the SLO Director image with:

```bash
docker build -f build/slo-director.Dockerfile \
  -t spandana/slo-director:local .
```

## Configure direct metric delivery

Replace `sro-host.example` in
[`deploy/kubernetes/apps/common/sro-endpoints.yaml`](deploy/kubernetes/apps/common/sro-endpoints.yaml)
with the hostname or IP of the machine running the SRO processes.

| Service | SRO ingest port |
| --- | ---: |
| ratings | 8087 |
| productpage | 8088 |
| details | 8089 |
| reviews | 8090 |Now 
| compression | 8091 |
| imgproc | 8092 |

These ports must be reachable from the application pods and correspond to
`ingestAddress` in `configs/sro/`.

## Deploy an application

The application manifests expect a Secret named
`spandana-serverless-backends`. Populate the URL keys for the applications you
deploy: `ratings-url`, `details-url`, `compression-url`, and `imgproc-url`.
Serverless endpoints are deployment-specific and are not stored in this
repository.

The manifests also expect an EFS-backed PVC named `efs-claim` for persistent
SLO Director logs. See [Deploying applications](docs/deploying-apps.md) for
copyable Secret and EFS examples, ConfigMap key wiring, and verification steps.

Apply the common ServiceAccount and endpoint ConfigMap before an application:

```bash
kubectl apply -f deploy/kubernetes/apps/common/
kubectl apply -f deploy/kubernetes/apps/ratings/
```

Replace `ratings` with `compression`, `details`, or `imgproc` as needed. Review
environment-specific cloud settings before applying the manifests.

## Run Spandana

Set `KUBECONFIG` when the target cluster is not already the kubectl default:

```bash
export KUBECONFIG=/path/to/kubeconfig
```

Run one service:

```bash
./scripts/start-sro.sh configs/sro/ratings.json
```

Run a service chain:

```bash
./scripts/start-sro.sh \
  configs/sro/productpage.json \
  configs/sro/details.json \
  configs/sro/reviews.json \
  configs/sro/ratings.json
```

The launcher starts one resource manager, one scaling monitor, and one SRO tmux
window per config. Productpage and reviews configs are included, but their
application manifests come from the underlying Bookinfo deployment. Results
are written under `artifacts/` and isolated by service.

See [Running SRO](docs/running-sro.md) for endpoint configuration, environment
variables, output settings, and tmux commands.

## Workload traces

Generated traces are distributed separately and are intentionally not stored
in Git. See [Workload traces](traces/README.md) for the k6 replay command.


## Citation

```bibtex
@inproceedings{dehigama2026spandana,
  author    = {Dilina Dehigama and Shyam Jesalpura and Zeyu Xu and
               Marton Nemeth and Shengda Zhu and Marios Kogias and Boris Grot},
  title     = {{Spandana}: Reconciling Strict {SLOs} with Low Cost under
               Fine-Grained Load Fluctuations},
  booktitle = {Proceedings of the ACM Symposium on Cloud Computing (SoCC)},
  year      = {2026},
  address   = {Singapore},
  note      = {To appear}
}
```

The DOI and final proceedings metadata will be added after publication.
