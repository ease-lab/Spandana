#!/usr/bin/env bash
set -euo pipefail

: "${EKS_CLUSTER_NAME:?Set EKS_CLUSTER_NAME to the target EKS cluster name}"
: "${AWS_REGION:?Set AWS_REGION to the cluster AWS region}"
: "${AWS_VPC_ID:?Set AWS_VPC_ID to the cluster VPC ID}"

RELEASE_NAME="${AWS_LB_CONTROLLER_RELEASE:-aws-load-balancer-controller}"
NAMESPACE="${AWS_LB_CONTROLLER_NAMESPACE:-kube-system}"
SERVICE_ACCOUNT="${AWS_LB_CONTROLLER_SERVICE_ACCOUNT:-aws-load-balancer-controller}"

version_args=()
if [[ -n "${AWS_LB_CONTROLLER_CHART_VERSION:-}" ]]; then
  version_args=(--version "${AWS_LB_CONTROLLER_CHART_VERSION}")
fi

helm repo add eks https://aws.github.io/eks-charts --force-update
helm repo update eks

helm upgrade --install "${RELEASE_NAME}" eks/aws-load-balancer-controller \
  --namespace "${NAMESPACE}" \
  --set "clusterName=${EKS_CLUSTER_NAME}" \
  --set serviceAccount.create=false \
  --set "serviceAccount.name=${SERVICE_ACCOUNT}" \
  --set "region=${AWS_REGION}" \
  --set "vpcId=${AWS_VPC_ID}" \
  "${version_args[@]}"
