#!/usr/bin/env bash

set -Eeuo pipefail

readonly SCRIPT_DIR="$(
  cd -- "$(dirname -- "${BASH_SOURCE[0]}")"
  pwd
)"
readonly REPO_ROOT="$(
  cd -- "${SCRIPT_DIR}/../.."
  pwd
)"
readonly ACCEPTANCE_FILE="${REPO_ROOT}/compose.acceptance.yaml"
readonly API_PORT="${ACCEPTANCE_API_PORT:-18083}"
readonly RUN_ID="${ACCEPTANCE_RUN_ID:-$(date -u +%Y%m%dT%H%M%SZ)-$$}"
readonly PROJECT="clp-acceptance-${RUN_ID,,}"
readonly RESULT_DIR="${ACCEPTANCE_RESULT_DIR:-${REPO_ROOT}/artifacts/acceptance}"
readonly PRODUCER_IMAGE="$(
  printf '%s' \
    "${ACCEPTANCE_PRODUCER_IMAGE:-container-log-platform-log-producer:1.0.0}"
)"
readonly FIXTURE_SETTLE_SECONDS="${ACCEPTANCE_FIXTURE_SETTLE_SECONDS:-10}"

data_dir=""
normal_should_restore=0
acceptance_started=0
fixture_containers=()
fixture_serial=0

compose_normal() {
  docker compose \
    --project-name container-log-platform \
    --project-directory "${REPO_ROOT}" \
    --file "${REPO_ROOT}/compose.yaml" \
    "$@"
}

compose_acceptance() {
  docker compose \
    --project-name "${PROJECT}" \
    --project-directory "${REPO_ROOT}" \
    --file "${REPO_ROOT}/compose.yaml" \
    --file "${ACCEPTANCE_FILE}" \
    "$@"
}

cleanup() {
  exit_code=$?
  trap - EXIT
  set +e

  if [[ "${exit_code}" != "0" && "${acceptance_started}" == "1" ]]; then
    compose_acceptance ps \
      >"${RESULT_DIR}/${RUN_ID}-failure-compose-ps.txt" \
      2>&1
    compose_acceptance logs --no-color \
      >"${RESULT_DIR}/${RUN_ID}-failure-compose.log" \
      2>&1
  fi

  for container_id in "${fixture_containers[@]}"; do
    docker rm --force "${container_id}" >/dev/null 2>&1 || true
  done

  while IFS= read -r container_id; do
    if [[ -n "${container_id}" ]]; then
      docker rm --force "${container_id}" >/dev/null 2>&1 || true
    fi
  done < <(
    docker ps \
      --all \
      --quiet \
      --filter \
        "label=com.donking36.container_log_platform.acceptance_run=${RUN_ID}"
  )

  if [[ "${PROJECT}" == clp-acceptance-* ]]; then
    compose_acceptance down \
      --volumes \
      --remove-orphans \
      >/dev/null 2>&1 || true
  fi

  if [[ -n "${data_dir}" ]]; then
    case "${data_dir}" in
      /tmp/container-log-platform-e2e.*)
        rm -rf -- "${data_dir}"
        ;;
      *)
        printf 'refusing to remove unexpected data directory: %s\n' \
          "${data_dir}" >&2
        exit_code=1
        ;;
    esac
  fi

  if [[ "${normal_should_restore}" == "1" ]]; then
    if ! (
      cd "${REPO_ROOT}"
      compose_normal up \
        --detach \
        --wait \
        --wait-timeout 240
    ); then
      printf 'failed to restore the normal Compose project\n' >&2
      exit_code=1
    fi
  fi

  exit "${exit_code}"
}

trap cleanup EXIT

wait_ready() {
  local ready=0

  for _ in $(seq 1 300); do
    if curl \
      --noproxy '*' \
      --silent \
      --fail \
      --output /dev/null \
      "http://127.0.0.1:${API_PORT}/readyz"; then
      ready=1
      break
    fi

    sleep 0.1
  done

  if [[ "${ready}" != "1" ]]; then
    printf 'API did not become ready on port %s\n' "${API_PORT}" >&2
    return 1
  fi
}

wait_service_healthy() {
  local service="$1"
  local container_id
  local status

  for _ in $(seq 1 240); do
    container_id="$(compose_acceptance ps --quiet "${service}")"

    if [[ -n "${container_id}" ]]; then
      status="$(
        docker inspect \
          --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' \
          "${container_id}"
      )"

      if [[ "${status}" == "healthy" || "${status}" == "running" ]]; then
        return 0
      fi
    fi

    sleep 0.5
  done

  printf 'service did not become healthy: %s\n' "${service}" >&2
  return 1
}

generate_fixture() {
  local run_id="$1"
  local stdout_count="$2"
  local stderr_count="$3"
  local file_count="$4"
  fixture_serial=$((fixture_serial + 1))
  local container_name="clp-${run_id}-${fixture_serial}"
  local container_id
  local exit_code

  if ! [[ "${run_id}" =~ ^[a-z0-9-]+$ ]]; then
    printf 'fixture run ID contains unsafe characters: %s\n' \
      "${run_id}" >&2
    return 1
  fi

  container_id="$(
    docker run \
      --detach \
      --name "${container_name}" \
      --network none \
      --label \
        com.donking36.container_log_platform.collect=true \
      --label \
        "com.donking36.container_log_platform.acceptance_run=${RUN_ID}" \
      --log-driver json-file \
      --log-opt max-size=10m \
      --log-opt max-file=3 \
      --mount \
        "type=volume,source=${producer_volume},target=/logs" \
      --entrypoint /bin/sh \
      "${PRODUCER_IMAGE}" \
      -c '
        run_id="$1"
        stdout_count="$2"
        stderr_count="$3"
        file_count="$4"
        settle_seconds="$5"
        logged_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
        file_path="/logs/${run_id}.ndjson"
        file_marker="${run_id##*-}"

        index=1
        while [ "${index}" -le "${stdout_count}" ]; do
          printf \
            "{\"source_event_id\":\"%s-stdout-%06d\",\"sequence\":%d,\"level\":\"INFO\",\"message\":\"stdout event %06d\",\"logged_at\":\"%s\"}\n" \
            "${run_id}" \
            "${index}" \
            "${index}" \
            "${index}" \
            "${logged_at}"
          index=$((index + 1))
        done

        index=1
        while [ "${index}" -le "${stderr_count}" ]; do
          printf \
            "{\"source_event_id\":\"%s-stderr-%06d\",\"sequence\":%d,\"level\":\"ERROR\",\"message\":\"stderr event %06d\",\"logged_at\":\"%s\"}\n" \
            "${run_id}" \
            "${index}" \
            "${index}" \
            "${index}" \
            "${logged_at}" \
            >&2
          index=$((index + 1))
        done

        index=1
        while [ "${index}" -le "${file_count}" ]; do
          printf \
            "{\"fixture_file_id\":\"%s\",\"source_event_id\":\"%s-file-%06d\",\"sequence\":%d,\"level\":\"WARN\",\"message\":\"file event %06d\",\"logged_at\":\"%s\"}\n" \
            "${file_marker}" \
            "${run_id}" \
            "${index}" \
            "${index}" \
            "${index}" \
            "${logged_at}" \
            >>"${file_path}"
          index=$((index + 1))
        done

        sync
        sleep "${settle_seconds}"
      ' \
      -- \
      "${run_id}" \
      "${stdout_count}" \
      "${stderr_count}" \
      "${file_count}" \
      "${FIXTURE_SETTLE_SECONDS}"
  )"

  fixture_containers+=("${container_id}")
  docker wait "${container_id}" >/dev/null

  exit_code="$(
    docker inspect \
      --format '{{.State.ExitCode}}' \
      "${container_id}"
  )"

  if [[ "${exit_code}" != "0" ]]; then
    docker logs "${container_id}" >&2 || true
    printf 'fixture container exited with code %s\n' "${exit_code}" >&2
    return 1
  fi
}

sum_duplicates_since() {
  local since="$1"

  compose_acceptance logs \
    --since "${since}" \
    --no-color \
    api \
    | python3 -c '
import json
import sys

total = 0
for line in sys.stdin:
    start = line.find("{")
    if start < 0:
        continue
    try:
        event = json.loads(line[start:])
    except json.JSONDecodeError:
        continue
    if event.get("operation") == "log_ingestion":
        total += int(event.get("duplicated", 0))

print(total)
'
}

wait_for_duplicates() {
  local since="$1"
  local expected="$2"
  local total

  for _ in $(seq 1 180); do
    total="$(sum_duplicates_since "${since}")"

    if [[ "${total}" =~ ^[0-9]+$ && "${total}" -ge "${expected}" ]]; then
      printf '%s\n' "${total}"
      return 0
    fi

    sleep 1
  done

  printf 'duplicate count did not reach %s\n' "${expected}" >&2
  return 1
}

verify_events() {
  local stage="$1"
  local expected="$2"
  local stdout_count="$3"
  local stderr_count="$4"
  local file_count="$5"
  local result_file="${RESULT_DIR}/${RUN_ID}-${stage}.json"
  local error_file="${RESULT_DIR}/${RUN_ID}-${stage}.stderr.log"

  go run ./test/e2e \
    -base-url "http://127.0.0.1:${API_PORT}" \
    -prefix "${event_prefix}" \
    -start "${window_start}" \
    -end "${window_end}" \
    -expected "${expected}" \
    -stdout "${stdout_count}" \
    -stderr "${stderr_count}" \
    -file "${file_count}" \
    -timeout 3m \
    2> >(tee "${error_file}" >&2) \
    | tee "${result_file}"
}

queue_events() {
  pipeline_metric "pipelines.main.queue.events_count"
}

pipeline_events_in() {
  pipeline_metric "pipelines.main.events.in"
}

pipeline_events_out() {
  pipeline_metric "pipelines.main.events.out"
}

pipeline_metric() {
  local path="$1"

  compose_acceptance exec -T logstash \
    curl -fsS \
    http://127.0.0.1:9600/_node/stats/pipelines/main \
    | python3 -c '
import json
import sys

data = json.load(sys.stdin)
value = data
for key in sys.argv[1].split("."):
    value = value[key]
print(value)
' "${path}"
}

wait_for_input_delta() {
  local baseline="$1"
  local expected_delta="$2"
  local current

  for _ in $(seq 1 120); do
    current="$(pipeline_events_in 2>/dev/null || printf '0')"

    if [[ "${current}" =~ ^[0-9]+$ &&
      $((current - baseline)) -ge "${expected_delta}" ]]; then
      printf '%s\n' "${current}"
      return 0
    fi

    sleep 0.5
  done

  printf 'Logstash events.in did not increase by %s\n' \
    "${expected_delta}" >&2
  return 1
}

wait_for_queue_nonzero() {
  local count

  for _ in $(seq 1 120); do
    count="$(queue_events 2>/dev/null || printf '0')"

    if [[ "${count}" =~ ^[0-9]+$ && "${count}" -gt 0 ]]; then
      printf '%s\n' "${count}"
      return 0
    fi

    sleep 0.5
  done

  printf 'Logstash persistent queue did not accumulate events\n' >&2
  return 1
}

wait_for_output() {
  local expected="$1"
  local count

  for _ in $(seq 1 120); do
    count="$(pipeline_events_out 2>/dev/null || printf '0')"

    if [[ "${count}" =~ ^[0-9]+$ && "${count}" -ge "${expected}" ]]; then
      printf '%s\n' "${count}"
      return 0
    fi

    sleep 0.5
  done

  printf 'Logstash events.out did not reach %s\n' "${expected}" >&2
  return 1
}

wait_for_queue_zero() {
  local count

  for _ in $(seq 1 120); do
    count="$(queue_events 2>/dev/null || printf '1')"

    if [[ "${count}" == "0" ]]; then
      return 0
    fi

    sleep 0.5
  done

  printf 'Logstash persistent queue did not drain to zero\n' >&2
  return 1
}

cd "${REPO_ROOT}"

for command_name in docker go curl python3; do
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    printf 'required command not found: %s\n' "${command_name}" >&2
    exit 1
  fi
done

if ! [[ "${RUN_ID}" =~ ^[A-Za-z0-9-]+$ ]]; then
  printf 'ACCEPTANCE_RUN_ID contains unsafe characters\n' >&2
  exit 1
fi

if ! [[ "${API_PORT}" =~ ^[0-9]+$ ]] ||
  [[ "${API_PORT}" -lt 1 || "${API_PORT}" -gt 65535 ]]; then
  printf 'ACCEPTANCE_API_PORT must be between 1 and 65535\n' >&2
  exit 1
fi

if ! [[ "${PROJECT}" == clp-acceptance-* ]]; then
  printf 'refusing unsafe Compose project name: %s\n' "${PROJECT}" >&2
  exit 1
fi

mkdir -p "${RESULT_DIR}"
data_dir="$(mktemp -d /tmp/container-log-platform-e2e.XXXXXX)"

export ACCEPTANCE_DATA_DIR="${data_dir}"
export ACCEPTANCE_API_PORT="${API_PORT}"
export APP_UID="${APP_UID:-$(id -u)}"
export APP_GID="${APP_GID:-$(id -g)}"

if [[ -n "$(compose_normal ps --status running --quiet)" ]]; then
  normal_should_restore=1
fi

if [[ -n "$(compose_normal ps --all --quiet)" ]]; then
  compose_normal down
fi

foreign_collectors="$(
  docker ps \
    --filter \
      label=com.donking36.container_log_platform.collect=true \
    --format '{{.ID}} {{.Names}}'
)"
if [[ -n "${foreign_collectors}" ]]; then
  printf 'another labeled log source is running; stop it before acceptance:\n' \
    >&2
  printf '%s\n' "${foreign_collectors}" >&2
  exit 1
fi

compose_acceptance config --quiet
acceptance_started=1
compose_acceptance up \
  --detach \
  --build \
  --wait \
  --wait-timeout 240
compose_acceptance ps \
  | tee "${RESULT_DIR}/${RUN_ID}-compose-ps.txt"
wait_ready

# The regular producer proves one-click startup and health. Fixed fixtures below
# provide the exact 1000 -> 1200 -> 1300 acceptance counts.
compose_acceptance stop log-producer

producer_volume="$(
  docker volume ls \
    --quiet \
    --filter "label=com.docker.compose.project=${PROJECT}" \
    --filter "label=com.docker.compose.volume=producer-logs"
)"
if [[ -z "${producer_volume}" ]]; then
  printf 'could not locate acceptance producer volume\n' >&2
  exit 1
fi

event_prefix="acceptance-${RUN_ID,,}"
window_start="$(date -u --date='-10 seconds' +%Y-%m-%dT%H:%M:%SZ)"
window_end="$(date -u --date='+30 minutes' +%Y-%m-%dT%H:%M:%SZ)"

generate_fixture "${event_prefix}-normal" 400 100 500
verify_events at001-e2e 1000 400 100 500

replay_started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
generate_fixture "${event_prefix}-normal" 400 100 500
duplicate_count="$(wait_for_duplicates "${replay_started_at}" 1000)"
printf 'duplicated_events=%s\n' "${duplicate_count}" \
  | tee "${RESULT_DIR}/${RUN_ID}-at001-idempotency.txt"
compose_acceptance logs \
  --since "${replay_started_at}" \
  --no-color \
  api \
  >"${RESULT_DIR}/${RUN_ID}-at001-idempotency-api.log"
verify_events at001-idempotency 1000 400 100 500

input_before="$(pipeline_events_in)"
compose_acceptance stop api
generate_fixture "${event_prefix}-downstream" 80 20 100

input_after="$(wait_for_input_delta "${input_before}" 200)"
queue_count="$(wait_for_queue_nonzero)"
printf 'events_in_before=%s\nevents_in_after=%s\nqueue_events_before_recreate=%s\n' \
  "${input_before}" \
  "${input_after}" \
  "${queue_count}" \
  | tee "${RESULT_DIR}/${RUN_ID}-at002-queue.txt"

compose_acceptance stop filebeat
compose_acceptance up \
  --detach \
  --no-deps \
  --force-recreate \
  logstash
wait_service_healthy logstash

recreated_input="$(pipeline_events_in)"
recreated_queue="$(queue_events)"
if [[ "${recreated_input}" != "0" ]] ||
  ! [[ "${recreated_queue}" =~ ^[0-9]+$ ]] ||
  [[ "${recreated_queue}" -le 0 ]]; then
  printf 'unexpected PQ state after Logstash recreate: events.in=%s queue=%s\n' \
    "${recreated_input}" \
    "${recreated_queue}" \
    >&2
  exit 1
fi

compose_acceptance exec -T logstash \
  curl -fsS \
  http://127.0.0.1:9600/_node/stats/pipelines/main \
  >"${RESULT_DIR}/${RUN_ID}-at002-after-logstash-recreate.json"

compose_acceptance start api
wait_ready
verify_events at002-downstream-recovery 1200 480 120 600
output_after="$(wait_for_output 200)"
wait_for_queue_zero
printf 'events_in_after_recreate=%s\nqueue_after_recreate=%s\nevents_out_after_recovery=%s\nqueue_after_recovery=0\n' \
  "${recreated_input}" \
  "${recreated_queue}" \
  "${output_after}" \
  | tee -a "${RESULT_DIR}/${RUN_ID}-at002-queue.txt"

compose_acceptance up \
  --detach \
  --no-deps \
  --force-recreate \
  filebeat
wait_service_healthy filebeat
generate_fixture "${event_prefix}-after-filebeat" 40 10 50
verify_events at003-filebeat-recovery 1300 520 130 650

compose_acceptance up \
  --detach \
  --no-deps \
  --force-recreate \
  api
wait_ready
verify_events at005-sqlite-persistence 1300 520 130 650

request_id="${event_prefix}-request"
headers_file="${RESULT_DIR}/${RUN_ID}-at010-headers.txt"
body_file="${RESULT_DIR}/${RUN_ID}-at010-body.json"
status_code="$(
  curl \
    --noproxy '*' \
    --silent \
    --show-error \
    --dump-header "${headers_file}" \
    --output "${body_file}" \
    --write-out '%{http_code}' \
    --header "X-Request-ID: ${request_id}" \
    "http://127.0.0.1:${API_PORT}/api/v1/logs?page=0"
)"

if [[ "${status_code}" != "400" ]] ||
  ! grep -Fqi "X-Request-ID: ${request_id}" "${headers_file}" ||
  ! grep -Fq "\"request_id\":\"${request_id}\"" "${body_file}"; then
  printf 'request ID acceptance check failed\n' >&2
  exit 1
fi

sleep 0.5
compose_acceptance logs --no-color api \
  | grep -F "${request_id}" \
  >"${RESULT_DIR}/${RUN_ID}-at010-api.log"

if [[ ! -s "${RESULT_DIR}/${RUN_ID}-at010-api.log" ]]; then
  printf 'request ID was not found in API logs\n' >&2
  exit 1
fi

(
  end_second=$((SECONDS + 3))
  while [[ "${SECONDS}" -lt "${end_second}" ]]; do
    curl \
      --noproxy '*' \
      --silent \
      --output /dev/null \
      "http://127.0.0.1:${API_PORT}/api/v1/logs?page=1&page_size=100" \
      || true
  done
) &
query_load_pid=$!

api_container_id="$(compose_acceptance ps --quiet api)"
docker stop --time 15 "${api_container_id}" >/dev/null
wait "${query_load_pid}" || true

api_exit_code="$(
  docker inspect \
    --format '{{.State.ExitCode}}' \
    "${api_container_id}"
)"
printf 'api_exit_code=%s\n' "${api_exit_code}" \
  | tee "${RESULT_DIR}/${RUN_ID}-at009-graceful-shutdown.txt"

if [[ "${api_exit_code}" != "0" ]]; then
  printf 'API did not exit cleanly after SIGTERM\n' >&2
  exit 1
fi

compose_acceptance start api
wait_ready
verify_events at009-after-graceful-shutdown 1300 520 130 650

printf 'Compose acceptance passed: %s\n' "${RUN_ID}"
