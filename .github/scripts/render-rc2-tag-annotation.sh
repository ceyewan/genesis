#!/usr/bin/env bash

set -euo pipefail

required_variables=(
  CANDIDATE_TAG
  RESONANCE_SHA
  STAGE3_MANIFEST_PATH
  STAGE3_MANIFEST_SHA256
  STAGE3_TESTED_RESONANCE_SHA
  STAGE3_TESTED_AT
  RESONANCE_MAIN_TIP
  RESONANCE_MAIN_CHECKED_AT
  GITHUB_RUN_ID
  EVIDENCE_ATTEMPT
  GITHUB_WORKFLOW_SHA
  EVIDENCE_DIGEST
  EVIDENCE_ID
  EVIDENCE_NAME
  EVIDENCE_SIZE
  NORMAL_DIGEST
  NORMAL_ID
  NORMAL_NAME
  NORMAL_ATTEMPT
  NORMAL_SIZE
  RACE_DIGEST
  RACE_ID
  RACE_NAME
  RACE_ATTEMPT
  RACE_SIZE
  API_DIGEST
  API_ID
  API_NAME
  API_ATTEMPT
  API_SIZE
  CONSUMER_DIGEST
  CONSUMER_ID
  CONSUMER_NAME
  CONSUMER_ATTEMPT
  CONSUMER_SIZE
)

for variable in "${required_variables[@]}"; do
  if [[ -z "${!variable:-}" ]]; then
    echo "required annotation variable is empty: ${variable}" >&2
    exit 1
  fi
done

printf '%s\n' \
  "Genesis ${CANDIDATE_TAG}" \
  "" \
  "Resonance-SHA: ${RESONANCE_SHA}" \
  "Stage3-Manifest-Path: ${STAGE3_MANIFEST_PATH}" \
  "Stage3-Manifest-SHA256: ${STAGE3_MANIFEST_SHA256}" \
  "Stage3-Tested-Resonance-SHA: ${STAGE3_TESTED_RESONANCE_SHA}" \
  "Stage3-Tested-At: ${STAGE3_TESTED_AT}" \
  "Resonance-Main-Tip-At-Publish: ${RESONANCE_MAIN_TIP}" \
  "Resonance-Main-Checked-At: ${RESONANCE_MAIN_CHECKED_AT}" \
  "Preflight-Run: ${GITHUB_RUN_ID}" \
  "Preflight-Attempt: ${EVIDENCE_ATTEMPT}" \
  "Workflow-SHA: ${GITHUB_WORKFLOW_SHA}" \
  "evidence_artifact_sha256=${EVIDENCE_DIGEST}" \
  "evidence_artifact_id=${EVIDENCE_ID}" \
  "evidence_artifact_name=${EVIDENCE_NAME}" \
  "evidence_artifact_attempt=${EVIDENCE_ATTEMPT}" \
  "evidence_artifact_size=${EVIDENCE_SIZE}" \
  "" \
  "normal_artifact_sha256=${NORMAL_DIGEST}" \
  "normal_artifact_id=${NORMAL_ID}" \
  "normal_artifact_name=${NORMAL_NAME}" \
  "normal_artifact_attempt=${NORMAL_ATTEMPT}" \
  "normal_artifact_size=${NORMAL_SIZE}" \
  "race_artifact_sha256=${RACE_DIGEST}" \
  "race_artifact_id=${RACE_ID}" \
  "race_artifact_name=${RACE_NAME}" \
  "race_artifact_attempt=${RACE_ATTEMPT}" \
  "race_artifact_size=${RACE_SIZE}" \
  "api_artifact_sha256=${API_DIGEST}" \
  "api_artifact_id=${API_ID}" \
  "api_artifact_name=${API_NAME}" \
  "api_artifact_attempt=${API_ATTEMPT}" \
  "api_artifact_size=${API_SIZE}" \
  "consumer_artifact_sha256=${CONSUMER_DIGEST}" \
  "consumer_artifact_id=${CONSUMER_ID}" \
  "consumer_artifact_name=${CONSUMER_NAME}" \
  "consumer_artifact_attempt=${CONSUMER_ATTEMPT}" \
  "consumer_artifact_size=${CONSUMER_SIZE}"
