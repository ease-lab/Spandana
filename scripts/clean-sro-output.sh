#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"

rm -f "${REPO_ROOT}"/artifacts/sro/*.csv
rm -f "${REPO_ROOT}"/artifacts/sro/*.txt
rm -f "${REPO_ROOT}"/artifacts/sro/*.log
rm -f "${REPO_ROOT}"/artifacts/resource-manager/*.log
rm -f "${REPO_ROOT}"/artifacts/monitoring/*.log
