#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
UBUNTU_MANIFEST="${UBUNTU_MANIFEST:-${REPO_ROOT}/deploy/kubernetes/utilities/ubuntu.yaml}"
APP_LOG_PATH="/app/logs"

# Get optional deployment name from command line
DEPLOYMENT_NAME=${1:-}

# Step 0: Ensure Ubuntu pod is deployed and ready
UBUNTU_POD=$(kubectl get pods -l app=ubuntu -o jsonpath="{.items[0].metadata.name}")

if [ -z "$UBUNTU_POD" ]; then
    echo "[INFO] Ubuntu pod not found. Deploying from ${UBUNTU_MANIFEST}..."
    kubectl apply -f "${UBUNTU_MANIFEST}"

    echo "[INFO] Waiting for Ubuntu pod to be ready..."
    while true; do
        UBUNTU_POD=$(kubectl get pods -l app=ubuntu -o jsonpath="{.items[0].metadata.name}")
        STATUS=$(kubectl get pod "$UBUNTU_POD" -o jsonpath="{.status.phase}")
        if [ "$STATUS" == "Running" ]; then
            READY=$(kubectl get pod "$UBUNTU_POD" -o jsonpath="{.status.containerStatuses[0].ready}")
            if [ "$READY" == "true" ]; then
                echo "[INFO] Ubuntu pod is ready: $UBUNTU_POD"
                break
            fi
        fi
        sleep 2
    done
else
    echo "[INFO] Found existing Ubuntu pod: $UBUNTU_POD"
fi

# Step 1: Clean logs
if [ -n "$DEPLOYMENT_NAME" ]; then
    echo "[INFO] Cleaning logs for deployment: $DEPLOYMENT_NAME inside pod: $UBUNTU_POD"
    kubectl exec -it "$UBUNTU_POD" -- bash -c \
        "cd $APP_LOG_PATH/$DEPLOYMENT_NAME && rm -rf *.txt *.log && rm -rf */ && echo '[INFO] Deployment logs cleaned.'"
else
    echo "[INFO] Cleaning ALL logs inside Ubuntu pod: $UBUNTU_POD"
    kubectl exec -it "$UBUNTU_POD" -- bash -c \
        "cd $APP_LOG_PATH && rm -rf *.txt *.log && rm -rf */ && echo '[INFO] All logs cleaned.'"
fi
