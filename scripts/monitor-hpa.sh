#!/usr/bin/env bash
set -euo pipefail

namespace="${1:-default}"

echo "Monitoring HPA scaling events in namespace: ${namespace}"
echo "Press Ctrl+C to stop."

kubectl get events -n "${namespace}" --watch \
  --field-selector involvedObject.kind=HorizontalPodAutoscaler \
  | while read -r line; do
      if echo "${line}" | grep -E "SuccessfulRescale|ScalingReplicaSet|New size"; then
        echo "$(date '+%Y-%m-%d %H:%M:%S') | ${line}"
      fi
    done
