#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
LOG_DIR="${REPO_ROOT}/artifacts/monitoring"
LOG_FILE="${LOG_DIR}/scaling-events.log"
INTERVAL="${INTERVAL:-15}"

mkdir -p "${LOG_DIR}"

while true; do
  timestamp="$(date '+%Y-%m-%d %H:%M:%S')"
  echo "[${timestamp}] Checking for scaling events..." >> "${LOG_FILE}"

  kubectl get events --all-namespaces --sort-by=.lastTimestamp \
    | grep -E "Scaled up|Scaled down|ScaleUp|ScaleDown|removing node|adding node" \
    >> "${LOG_FILE}" 2>&1 || true

  echo >> "${LOG_FILE}"
  sleep "${INTERVAL}"
done
