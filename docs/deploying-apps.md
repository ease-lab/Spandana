# Deploying Spandana applications

Each application pod needs three shared Kubernetes resources in the same
namespace:

1. `spandana-serverless-backends`, a Secret containing serverless fallback
   URLs.
2. `spandana-sro-endpoints`, a ConfigMap containing the matching SRO ingest
   URL.
3. `efs-claim`, a PersistentVolumeClaim used to persist SLO Director logs.

The checked-in application manifests use the `default` namespace.

## Configure serverless backends

Copy the example outside the repository so real endpoints are not accidentally
committed:

```bash
cp deploy/kubernetes/examples/serverless-backends-secret.yaml \
  /tmp/spandana-serverless-backends.yaml
```

Edit the copy, replace the example URLs, and keep the keys for the applications
being deployed. `stringData` allows plain text input; Kubernetes performs the
Secret encoding when the manifest is applied.

```bash
kubectl apply -f /tmp/spandana-serverless-backends.yaml
```

The application-to-key mapping is:

| Application | Secret key |
| --- | --- |
| ratings | `ratings-url` |
| details | `details-url` |
| compression | `compression-url` |
| imgproc | `imgproc-url` |

Do not commit the populated Secret. Kubernetes Secrets are base64-encoded by
default and should not be treated as encrypted at rest unless the cluster is
configured accordingly.

## Configure SRO endpoints

Edit `deploy/kubernetes/apps/common/sro-endpoints.yaml` and replace
`sro-host.example` with a hostname or IP reachable from application pods. Each
URL must include `/metrics/ingest`, and its port must match `ingestAddress` in
the corresponding `configs/sro/<application>.json` file.

| Application | ConfigMap key | Default port |
| --- | --- | ---: |
| ratings | `ratings` | 8087 |
| productpage | `productpage` | 8088 |
| details | `details` | 8089 |
| reviews | `reviews` | 8090 |
| compression | `compression` | 8091 |
| imgproc | `imgproc` | 8092 |

Apply the ConfigMap:

```bash
kubectl apply -f deploy/kubernetes/apps/common/sro-endpoints.yaml
```

The proxy container selects one value using `configMapKeyRef`. For example,
the ratings deployment contains:

```yaml
- name: SRO_INGEST_URL
  valueFrom:
    configMapKeyRef:
      name: spandana-sro-endpoints
      key: ratings
```

The `key` must exist under the ConfigMap's `data` field. Change both the
ConfigMap key and this reference when adding a new application. Restart an
existing deployment after changing the ConfigMap because environment variables
are populated only when a pod starts:

```bash
kubectl rollout restart deployment/ratings
```

## Persist logs on Amazon EFS

The SLO Director writes request and event logs below
`/app/logs/<deployment-name>/`. Every application manifest mounts the volume
named `efs-volume` at `/app/logs` and expects a PVC named `efs-claim`.

### Prerequisites

- An existing EFS filesystem in the cluster VPC.
- An EFS mount target in every Availability Zone used by worker nodes. Its
  security group must allow inbound TCP/NFS port 2049 from the worker-node
  security group.
- The Amazon EFS CSI driver installed in the cluster with its required IAM
  permissions.

AWS documents CSI driver installation and supported provisioning modes in the
[Amazon EKS EFS guide](https://docs.aws.amazon.com/eks/latest/userguide/efs-csi.html).
The EFS networking requirements are covered in the
[EFS security-group guide](https://docs.aws.amazon.com/efs/latest/ug/network-access.html).

### Create the PV and PVC

Copy the static-provisioning example and replace `fs-REPLACE_ME` with the EFS
filesystem ID:

```bash
cp deploy/kubernetes/examples/efs-static-pv-pvc.yaml \
  /tmp/spandana-efs-static.yaml
```

To restrict Spandana to an existing EFS access point, set `volumeHandle` to
`<filesystem-id>::<access-point-id>`, for example
`fs-0123456789abcdef0::fsap-0123456789abcdef0`.

Apply the edited copy and wait for the claim to bind:

```bash
kubectl apply -f /tmp/spandana-efs-static.yaml
kubectl get pv spandana-efs-pv
kubectl get pvc efs-claim -n default
```

Both resources should report `Bound`. The example uses `ReadWriteMany`, which
allows replicas on different nodes to share the filesystem, and `Retain`, so
deleting the Kubernetes claim does not delete the EFS data.

### Deploy and verify

After configuring the Secret, ConfigMap, and PVC, deploy the common resources
and an application:

```bash
kubectl apply -f deploy/kubernetes/apps/common/
kubectl apply -f deploy/kubernetes/apps/ratings/
```

Confirm that the proxy can see the mounted directory:

```bash
POD_NAME=$(kubectl get pod -l app=ratings -o jsonpath='{.items[0].metadata.name}')
kubectl exec "$POD_NAME" -c proxy -- ls -la /app/logs/ratings
```

If a pod remains `Pending`, inspect `kubectl describe pod` and
`kubectl describe pvc efs-claim`. Common causes are a missing CSI driver, an
unbound PVC, missing EFS mount targets, or blocked TCP port 2049.
