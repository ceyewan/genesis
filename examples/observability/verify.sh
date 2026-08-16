#!/usr/bin/env bash

set -euo pipefail

gateway_url="${GATEWAY_URL:-http://127.0.0.1:8080}"
gateway_metrics_url="${GATEWAY_METRICS_URL:-http://127.0.0.1:9101/metrics}"
prometheus_url="${PROMETHEUS_URL:-http://127.0.0.1:9090}"
loki_url="${LOKI_URL:-http://127.0.0.1:3100}"
tempo_url="${TEMPO_URL:-http://127.0.0.1:3200}"
request_count="${REQUEST_COUNT:-3}"
poll_attempts="${POLL_ATTEMPTS:-30}"

if [[ ! "${request_count}" =~ ^[1-9][0-9]*$ ]] || [[ ! "${poll_attempts}" =~ ^[1-9][0-9]*$ ]]; then
  echo "REQUEST_COUNT 和 POLL_ATTEMPTS 必须是正整数" >&2
  exit 1
fi

for command_name in curl jq; do
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    echo "缺少依赖：${command_name}" >&2
    exit 1
  fi
done

wait_ready() {
  local name="$1"
  local url="$2"

  for ((attempt = 1; attempt <= poll_attempts; attempt++)); do
    if curl --fail --silent --show-error --output /dev/null "${url}"; then
      echo "[ok] ${name} ready"
      return 0
    fi
    sleep 2
  done

  echo "[fail] ${name} 未在限定时间内就绪：${url}" >&2
  return 1
}

prometheus_scalar() {
  local query="$1"
  curl --fail --silent --show-error --get "${prometheus_url}/api/v1/query" \
    --data-urlencode "query=${query}" |
    jq -r '.data.result[0].value[1] // "0"'
}

wait_ready "gateway metrics" "${gateway_metrics_url}"
wait_ready "Prometheus" "${prometheus_url}/-/ready"
wait_ready "Loki" "${loki_url}/ready"
wait_ready "Tempo" "${tempo_url}/ready"

baseline_http="$(prometheus_scalar 'sum(http_request_duration_seconds_count)')"
baseline_publish="$(prometheus_scalar 'sum({__name__="mq.publish_total"})')"
baseline_consume="$(prometheus_scalar 'sum({__name__="mq.consume_total"})')"

run_id="$(date +%s)"
first_order_id=""
for ((index = 1; index <= request_count; index++)); do
  response="$(curl --fail --silent --show-error \
    --request POST "${gateway_url}/orders" \
    --header 'Content-Type: application/json' \
    --header 'Authorization: Bearer demo-token' \
    --data "{\"user_id\":\"verify-${run_id}-${index}\",\"product_id\":\"A00${index}\"}")"
  order_id="$(jq -er '.order_id' <<<"${response}")"
  if [[ -z "${first_order_id}" ]]; then
    first_order_id="${order_id}"
  fi
  echo "[ok] order ${order_id}"
done

for ((attempt = 1; attempt <= poll_attempts; attempt++)); do
  up_json="$(curl --fail --silent --show-error --get "${prometheus_url}/api/v1/query" \
    --data-urlencode 'query=up{job="observability-demo"}')"
  healthy_targets="$(jq '[.data.result[] | select(.value[1] == "1")] | length' <<<"${up_json}")"
  http_count="$(prometheus_scalar 'sum(http_request_duration_seconds_count)')"
  publish_count="$(prometheus_scalar 'sum({__name__="mq.publish_total"})')"
  consume_count="$(prometheus_scalar 'sum({__name__="mq.consume_total"})')"

  if [[ "${healthy_targets}" -eq 3 ]] && awk "BEGIN {
    exit !((${http_count} - ${baseline_http}) >= ${request_count} &&
      (${publish_count} - ${baseline_publish}) >= ${request_count} &&
      (${consume_count} - ${baseline_consume}) >= ${request_count})
  }"; then
    break
  fi
  sleep 2
done

if [[ "${healthy_targets}" -ne 3 ]]; then
  echo "[fail] Prometheus 健康目标数为 ${healthy_targets}，期望 3" >&2
  exit 1
fi
if ! awk "BEGIN {
  exit !((${http_count} - ${baseline_http}) >= ${request_count} &&
    (${publish_count} - ${baseline_publish}) >= ${request_count} &&
    (${consume_count} - ${baseline_consume}) >= ${request_count})
}"; then
  echo "[fail] 指标增量未达到本次请求数：http=${http_count}-${baseline_http}, publish=${publish_count}-${baseline_publish}, consume=${consume_count}-${baseline_consume}" >&2
  exit 1
fi
http_delta="$(awk "BEGIN {print ${http_count} - ${baseline_http}}")"
publish_delta="$(awk "BEGIN {print ${publish_count} - ${baseline_publish}}")"
consume_delta="$(awk "BEGIN {print ${consume_count} - ${baseline_consume}}")"
echo "[ok] metrics targets=3 http_delta=${http_delta} publish_delta=${publish_delta} consume_delta=${consume_delta}"

log_query="{job=\"docker\"} | json | order_id=\"${first_order_id}\""
trace_id=""
log_services="[]"
for ((attempt = 1; attempt <= poll_attempts; attempt++)); do
  logs_json="$(curl --fail --silent --show-error --get "${loki_url}/loki/api/v1/query_range" \
    --data-urlencode "query=${log_query}" \
    --data-urlencode 'since=1h' \
    --data-urlencode 'limit=50' \
    --data-urlencode 'direction=forward')"
  log_services="$(jq -c '[.data.result[].values[][1] | fromjson? | select(.) | .["service.name"]] | unique' <<<"${logs_json}")"
  trace_id="$(jq -r '[.data.result[].values[][1] | fromjson? | select(.trace_id != null) | .trace_id][0] // ""' <<<"${logs_json}")"
  if [[ "$(jq 'length' <<<"${log_services}")" -eq 3 ]] && [[ -n "${trace_id}" ]]; then
    break
  fi
  sleep 2
done

if [[ "$(jq 'length' <<<"${log_services}")" -ne 3 ]] || [[ -z "${trace_id}" ]]; then
  echo "[fail] Loki 未检索到三服务日志或 trace_id：services=${log_services}" >&2
  exit 1
fi
echo "[ok] logs services=${log_services} trace_id=${trace_id}"

trace_services="[]"
span_count="0"
for ((attempt = 1; attempt <= poll_attempts; attempt++)); do
  if ! trace_json="$(curl --fail --silent "${tempo_url}/api/traces/${trace_id}")"; then
    sleep 2
    continue
  fi
  trace_services="$(jq -c '[.batches[].resource.attributes[] | select(.key == "service.name") | .value.stringValue] | unique' <<<"${trace_json}")"
  span_count="$(jq '[.batches[].scopeSpans[].spans[]] | length' <<<"${trace_json}")"
  if [[ "$(jq 'length' <<<"${trace_services}")" -eq 3 ]] && [[ "${span_count}" -ge 9 ]]; then
    break
  fi
  sleep 2
done

if [[ "$(jq 'length' <<<"${trace_services}")" -ne 3 ]] || [[ "${span_count}" -lt 9 ]]; then
  echo "[fail] Tempo Trace 不完整：services=${trace_services}, spans=${span_count}" >&2
  exit 1
fi
echo "[ok] trace services=${trace_services} spans=${span_count}"
echo "observability 自动验收通过"
