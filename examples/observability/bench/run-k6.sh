#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RESULT_DIR="${SCRIPT_DIR}/results"
LOAD_SCRIPT_LOCAL="${SCRIPT_DIR}/load.js"

mkdir -p "${RESULT_DIR}"

ts="$(date -u +%Y%m%dT%H%M%SZ)"
summary_file="${RESULT_DIR}/summary-${ts}.json"
metrics_file="${RESULT_DIR}/metrics-${ts}.json"
latest_summary="${RESULT_DIR}/summary-latest.json"
latest_metrics="${RESULT_DIR}/metrics-latest.json"
latest_meta="${RESULT_DIR}/run-latest.json"
profile="${LOAD_PROFILE:-fixed}"

run_with_local_k6() {
  k6 run "${LOAD_SCRIPT_LOCAL}" \
    --summary-export "${summary_file}" \
    --out "json=${metrics_file}" \
    "$@"
}

run_with_docker_k6() {
  local env_args=()
  local env_filter_cmd=("rg" "^LOAD_")

  if ! command -v rg >/dev/null 2>&1; then
    env_filter_cmd=("grep" "^LOAD_")
  fi

  while IFS='=' read -r key _; do
    env_args+=("-e" "${key}")
  done < <(env | "${env_filter_cmd[@]}" || true)

  if [[ -z "${LOAD_URL:-}" ]]; then
    env_args+=("-e" "LOAD_URL=http://host.docker.internal:8080/orders")
  fi

  docker run --rm -i \
    "${env_args[@]}" \
    -v "${SCRIPT_DIR}:/scripts" \
    -v "${RESULT_DIR}:/results" \
    grafana/k6:latest run /scripts/load.js \
      --summary-export "/results/$(basename "${summary_file}")" \
      --out "json=/results/$(basename "${metrics_file}")" \
      "$@"
}

echo "k6 profile: ${profile}"
echo "summary: ${summary_file}"
echo "metrics: ${metrics_file}"

set +e
if command -v k6 >/dev/null 2>&1; then
  run_with_local_k6 "$@"
  k6_exit=$?
else
  run_with_docker_k6 "$@"
  k6_exit=$?
fi
set -e

if [[ -f "${summary_file}" ]]; then
  cp "${summary_file}" "${latest_summary}"
fi

if [[ -f "${metrics_file}" ]]; then
  cp "${metrics_file}" "${latest_metrics}"
fi

cat > "${latest_meta}" <<EOF
{
  "generated_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "profile": "${profile}",
  "summary_file": "$(basename "${summary_file}")",
  "metrics_file": "$(basename "${metrics_file}")",
  "k6_exit_code": ${k6_exit}
}
EOF

echo "latest summary: ${latest_summary}"
echo "latest metadata: ${latest_meta}"

exit "${k6_exit}"
