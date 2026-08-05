# Supporting tools

The direct metric path does not require a telemetry stack. This directory
contains only the optional AWS Load Balancer Controller installer. Third-party
executables are not vendored.

## AWS Load Balancer Controller

The script assumes an IAM-enabled Kubernetes service account named
`aws-load-balancer-controller` already exists.

```bash
export EKS_CLUSTER_NAME=my-cluster
export AWS_REGION=us-east-1
export AWS_VPC_ID=vpc-0123456789abcdef0
./tools/aws-lb-controller/install.sh
```

Optional variables:

- `AWS_LB_CONTROLLER_RELEASE`
- `AWS_LB_CONTROLLER_NAMESPACE`
- `AWS_LB_CONTROLLER_SERVICE_ACCOUNT`
- `AWS_LB_CONTROLLER_CHART_VERSION`
