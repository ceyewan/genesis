#!/usr/bin/env bash

set -euo pipefail

if [[ "$#" -ne 1 ]]; then
  echo "usage: record-backend-dependencies.sh <output-file>" >&2
  exit 2
fi

output_file="$1"
output_dir="$(dirname "${output_file}")"
mkdir -p "${output_dir}"
temporary_file="$(mktemp "${output_dir}/backend-dependencies.XXXXXX")"
trap 'rm -f "${temporary_file}"' EXIT

record_image() {
  local backend="$1"
  local image="$2"
  local inspection image_id repo_digests
  local -a resolved_digests

  inspection="$(docker image inspect --format '{{.Id}}|{{join .RepoDigests ","}}' "${image}")"
  IFS='|' read -r image_id repo_digests <<< "${inspection}"
  [[ "${image_id}" =~ ^sha256:[0-9a-f]{64}$ ]]
  [[ -n "${repo_digests}" ]]
  IFS=',' read -r -a resolved_digests <<< "${repo_digests}"
  for repo_digest in "${resolved_digests[@]}"; do
    [[ "${repo_digest}" =~ ^[^[:space:],]+@sha256:[0-9a-f]{64}$ ]]
  done

  printf 'backend=%s image=%s image_id=%s repo_digests=%s\n' \
    "${backend}" "${image}" "${image_id}" "${repo_digests}" >> "${temporary_file}"
}

record_image redis "redis:7.2-alpine"
record_image postgres "postgres:17-alpine"
record_image mysql "mysql:8.0"
record_image etcd "quay.io/coreos/etcd:v3.5.12"
record_image nats "nats:2.10-alpine"
record_image kafka "confluentinc/confluent-local:7.5.0"

sqlite_driver_version="$(go list -m -f '{{.Version}}' gorm.io/driver/sqlite)"
sqlite_engine_version="$(go list -m -f '{{.Version}}' github.com/mattn/go-sqlite3)"
[[ "${sqlite_driver_version}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([+-].*)?$ ]]
[[ "${sqlite_engine_version}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([+-].*)?$ ]]
printf 'backend=sqlite module=%s version=%s\n' \
  "gorm.io/driver/sqlite" "${sqlite_driver_version}" >> "${temporary_file}"
printf 'backend=sqlite module=%s version=%s\n' \
  "github.com/mattn/go-sqlite3" "${sqlite_engine_version}" >> "${temporary_file}"

mv "${temporary_file}" "${output_file}"
trap - EXIT
