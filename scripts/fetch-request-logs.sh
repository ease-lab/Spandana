#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: ./scripts/fetch-request-logs.sh <deployment-name> [output-directory]

Copies completed SLO Director logs from the EFS debug pod and runs the request
log analysis pipeline. Source logs are never deleted.

Environment variables:
  NAMESPACE         Kubernetes namespace (default: default)
  DEBUG_POD         EFS debug pod name (default: ubuntu)
  DEBUG_CONTAINER   Debug container name (default: ubuntu)
  WINDOW_SECONDS    Rolling latency window (default: 1)
  LATENCY_FIELD     request or total (default: request)
  KUBECTL           kubectl executable (default: kubectl)
  PYTHON          Python executable (default: python3)
EOF
}

if [[ $# -lt 1 || $# -gt 2 ]]; then
  usage >&2
  exit 1
fi

DEPLOYMENT_NAME="$1"
if [[ ! "${DEPLOYMENT_NAME}" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]]; then
  echo "Invalid Kubernetes deployment name: ${DEPLOYMENT_NAME}" >&2
  exit 1
fi

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
ANALYSIS_DIR="${REPO_ROOT}/experiments/analysis/request-logs"
NAMESPACE="${NAMESPACE:-default}"
DEBUG_POD="${DEBUG_POD:-ubuntu}"
DEBUG_CONTAINER="${DEBUG_CONTAINER:-ubuntu}"
WINDOW_SECONDS="${WINDOW_SECONDS:-1}"
LATENCY_FIELD="${LATENCY_FIELD:-request}"
KUBECTL_BIN="${KUBECTL:-kubectl}"
PYTHON_BIN="${PYTHON:-python3}"
RUN_ID="$(date -u +%Y%m%dT%H%M%SZ)"
OUTPUT_DIR="${2:-${REPO_ROOT}/artifacts/request-logs/${DEPLOYMENT_NAME}/${RUN_ID}}"
RAW_DIR="${OUTPUT_DIR}/raw"
SOURCE_PATH="/app/logs/${DEPLOYMENT_NAME}"

if [[ "${LATENCY_FIELD}" != "request" && "${LATENCY_FIELD}" != "total" ]]; then
  echo "LATENCY_FIELD must be 'request' or 'total'" >&2
  exit 1
fi
if [[ ! "${WINDOW_SECONDS}" =~ ^[1-9][0-9]*$ ]]; then
  echo "WINDOW_SECONDS must be a positive integer" >&2
  exit 1
fi
if ! command -v "${KUBECTL_BIN}" >/dev/null 2>&1; then
  echo "kubectl executable not found: ${KUBECTL_BIN}" >&2
  exit 1
fi
if ! command -v "${PYTHON_BIN}" >/dev/null 2>&1; then
  echo "Python executable not found: ${PYTHON_BIN}" >&2
  exit 1
fi
if [[ -d "${OUTPUT_DIR}" && -n "$(find "${OUTPUT_DIR}" -mindepth 1 -print -quit)" ]]; then
  echo "Output directory is not empty: ${OUTPUT_DIR}" >&2
  exit 1
fi

pod_phase="$(
  "${KUBECTL_BIN}" get pod "${DEBUG_POD}" \
    -n "${NAMESPACE}" \
    -o jsonpath='{.status.phase}'
)"
if [[ "${pod_phase}" != "Running" ]]; then
  echo "Debug pod ${NAMESPACE}/${DEBUG_POD} is not Running (phase: ${pod_phase:-unknown})." >&2
  echo "Deploy it with: kubectl apply -f deploy/kubernetes/utilities/ubuntu.yaml" >&2
  exit 1
fi

"${PYTHON_BIN}" -c 'import matplotlib' >/dev/null 2>&1 || {
  echo "Missing Python dependencies. Install:" >&2
  echo "  ${PYTHON_BIN} -m pip install -r ${ANALYSIS_DIR}/requirements.txt" >&2
  exit 1
}

mkdir -p "${RAW_DIR}"
echo "Copying ${NAMESPACE}/${DEBUG_POD}:${SOURCE_PATH} to ${RAW_DIR}"
"${KUBECTL_BIN}" cp \
  "${NAMESPACE}/${DEBUG_POD}:${SOURCE_PATH}/." \
  "${RAW_DIR}" \
  -c "${DEBUG_CONTAINER}"

if ! find "${RAW_DIR}" -type f -name 'request_logs_*.txt' -print -quit | grep -q .; then
  echo "No request_logs_*.txt files were copied from ${SOURCE_PATH}" >&2
  exit 1
fi

"${PYTHON_BIN}" "${ANALYSIS_DIR}/aggregate_request_logs.py" \
  --input-dir "${RAW_DIR}" \
  --output "${OUTPUT_DIR}/aggregated_results.csv" \
  --latency-field "${LATENCY_FIELD}"

"${PYTHON_BIN}" "${ANALYSIS_DIR}/generate_statistics.py" \
  --input "${OUTPUT_DIR}/aggregated_results.csv" \
  --output "${OUTPUT_DIR}/statistics.csv" \
  --window-seconds "${WINDOW_SECONDS}"

"${PYTHON_BIN}" "${ANALYSIS_DIR}/plot_requests.py" \
  --input "${OUTPUT_DIR}/statistics.csv" \
  --output "${OUTPUT_DIR}/vm-vs-serverless-requests.png"

"${PYTHON_BIN}" "${ANALYSIS_DIR}/count_requests.py" \
  --input-dir "${RAW_DIR}" | tee "${OUTPUT_DIR}/request-counts.txt"

printf '\nAnalysis complete: %s\n' "${OUTPUT_DIR}"
