#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
TMUX_SESSION="${TMUX_SESSION:-spandana}"
OUTPUT_ROOT="${SPANDANA_OUTPUT_ROOT:-${REPO_ROOT}/artifacts/sro}"
RESOURCE_LOG="${SPANDANA_RESOURCE_LOG:-${REPO_ROOT}/artifacts/resource-manager/cpu_metrics.log}"
MONITOR_INTERVAL="${INTERVAL:-15}"

if tmux has-session -t "${TMUX_SESSION}" 2>/dev/null; then
  echo "tmux session already exists: ${TMUX_SESSION}" >&2
  exit 1
fi

config_arguments=("$@")
if [[ ${#config_arguments[@]} -eq 0 ]]; then
  config_arguments=("${REPO_ROOT}/configs/sro/ratings.json")
fi

declare -a config_files=()
declare -a window_names=()
declare -A seen_windows=()

for config_argument in "${config_arguments[@]}"; do
  if [[ "${config_argument}" = /* ]]; then
    config_file="${config_argument}"
  elif [[ -f "${config_argument}" ]]; then
    config_dir="$(cd -- "$(dirname -- "${config_argument}")" && pwd)"
    config_file="${config_dir}/$(basename -- "${config_argument}")"
  else
    config_file="${REPO_ROOT}/${config_argument}"
  fi

  if [[ ! -f "${config_file}" ]]; then
    echo "Configuration file not found: ${config_argument}" >&2
    exit 1
  fi

  config_name="$(basename -- "${config_file}" .json)"
  window_name="sro-${config_name//[^a-zA-Z0-9_-]/-}"
  if [[ -n "${seen_windows[${window_name}]+present}" ]]; then
    echo "Multiple configurations resolve to the same tmux window: ${window_name}" >&2
    exit 1
  fi

  seen_windows["${window_name}"]=1
  config_files+=("${config_file}")
  window_names+=("${window_name}")
done

mkdir -p "${OUTPUT_ROOT}" "$(dirname -- "${RESOURCE_LOG}")" "${REPO_ROOT}/artifacts/monitoring"

printf -v repo_root_q '%q' "${REPO_ROOT}"
printf -v resource_log_q '%q' "${RESOURCE_LOG}"
printf -v monitor_interval_q '%q' "${MONITOR_INTERVAL}"

kubeconfig_assignment=""
if [[ -n "${KUBECONFIG:-}" ]]; then
  printf -v kubeconfig_q '%q' "${KUBECONFIG}"
  kubeconfig_assignment=" KUBECONFIG=${kubeconfig_q}"
fi

tmux new-session -d -s "${TMUX_SESSION}" -n resource-manager \
  "cd ${repo_root_q} && env SPANDANA_RESOURCE_LOG=${resource_log_q}${kubeconfig_assignment} go run ./cmd/resource-manager"

tmux new-window -t "${TMUX_SESSION}" -n scaling-monitor \
  "cd ${repo_root_q} && env INTERVAL=${monitor_interval_q} bash ./scripts/monitor-scaling.sh"

sleep 3

for index in "${!config_files[@]}"; do
  config_file="${config_files[${index}]}"
  window_name="${window_names[${index}]}"
  output_dir="${OUTPUT_ROOT}/$(basename -- "${config_file}" .json)"

  printf -v config_file_q '%q' "${config_file}"
  printf -v output_dir_q '%q' "${output_dir}"

  tmux new-window -t "${TMUX_SESSION}" -n "${window_name}" \
    "cd ${repo_root_q} && env SPANDANA_OUTPUT_DIR=${output_dir_q} go run ./cmd/sro -config=${config_file_q}"
done

tmux attach-session -t "${TMUX_SESSION}"
