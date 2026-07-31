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
readonly IMAGE="${STARTUP_IMAGE:-container-log-platform-api:1.0.0}"
readonly PUBLIC_PORT="${STARTUP_PUBLIC_PORT:-18082}"
readonly RUNS="${STARTUP_RUNS:-5}"
readonly LIMIT_MS="${STARTUP_LIMIT_MS:-3000}"
readonly RUN_ID="${STARTUP_RUN_ID:-$(date -u +%Y%m%dT%H%M%SZ)-$$}"
readonly RESULT_DIR="${STARTUP_RESULT_DIR:-${REPO_ROOT}/artifacts/acceptance}"

data_dir=""
container_id=""

cleanup() {
  if [[ -n "${container_id}" ]]; then
    docker rm --force "${container_id}" >/dev/null 2>&1 || true
  fi

  if [[ -n "${data_dir}" ]]; then
    case "${data_dir}" in
      /tmp/container-log-platform-startup.*)
        rm -rf -- "${data_dir}"
        ;;
      *)
        printf 'refusing to remove unexpected data directory: %s\n' \
          "${data_dir}" >&2
        ;;
    esac
  fi
}

trap cleanup EXIT

cd "${REPO_ROOT}"

for command_name in docker curl date; do
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    printf 'required command not found: %s\n' "${command_name}" >&2
    exit 1
  fi
done

if ! [[ "${RUNS}" =~ ^[1-9][0-9]*$ ]]; then
  printf 'STARTUP_RUNS must be a positive integer\n' >&2
  exit 1
fi

if ! [[ "${LIMIT_MS}" =~ ^[1-9][0-9]*$ ]]; then
  printf 'STARTUP_LIMIT_MS must be a positive integer\n' >&2
  exit 1
fi

if ! [[ "${RUN_ID}" =~ ^[A-Za-z0-9._-]+$ ]]; then
  printf 'STARTUP_RUN_ID may contain only letters, digits, dot, underscore and hyphen\n' \
    >&2
  exit 1
fi

# 只有 5 次、3000ms 阈值属于正式基线；其他参数即使达到阈值也只标记
# EXPLORATORY 并最终返回非零，避免探索性结果被误当成正式验收证据。
baseline=1
if [[ "${RUNS}" != "5" || "${LIMIT_MS}" != "3000" ]]; then
  baseline=0
fi

if [[ "${baseline}" != "1" &&
  "${STARTUP_ALLOW_NON_BASELINE:-0}" != "1" ]]; then
  printf 'acceptance requires STARTUP_RUNS=5 and STARTUP_LIMIT_MS=3000; ' \
    >&2
  printf 'set STARTUP_ALLOW_NON_BASELINE=1 only for exploratory runs\n' \
    >&2
  exit 1
fi

mkdir -p "${RESULT_DIR}"

if [[ "${STARTUP_SKIP_BUILD:-0}" != "1" ]]; then
  docker compose build api
fi

data_dir="$(mktemp -d /tmp/container-log-platform-startup.XXXXXX)"
result_file="${RESULT_DIR}/${RUN_ID}-startup.csv"
environment_file="${RESULT_DIR}/${RUN_ID}-startup-environment.txt"

{
  printf 'run_id=%s\n' "${RUN_ID}"
  printf 'git_commit=%s\n' "$(git rev-parse HEAD)"
  printf 'image=%s\n' "${IMAGE}"
  printf 'image_id=%s\n' "$(
    docker image inspect --format '{{.Id}}' "${IMAGE}"
  )"
  printf 'runs=%s\n' "${RUNS}"
  printf 'limit_ms=%s\n' "${LIMIT_MS}"
  printf 'baseline_parameters=%s\n' "${baseline}"
  printf 'measurement=cold SQLite initialization on a fresh directory per run\n'
  printf 'wsl_kernel=%s\n' "$(uname -r)"
  uname -a
  docker version
  docker compose version
} >"${environment_file}"

printf 'run,started_at,ready_at,duration_ms,result\n' \
  | tee "${result_file}"

failed=0

# 每轮使用全新目录，测量包含首次建库和迁移的冷 SQLite 启动时间。
for run_number in $(seq 1 "${RUNS}"); do
  run_data_dir="${data_dir}/run-${run_number}"
  mkdir -p "${run_data_dir}"

  container_id="$(
    docker create \
    --publish "127.0.0.1:${PUBLIC_PORT}:8080" \
    --env APP_ENV=production \
    --env DATABASE_PATH=/app/data/startup.db \
    --volume "${run_data_dir}:/app/data" \
    "${IMAGE}" \
  )"

  docker start "${container_id}" >/dev/null

  # 以 Docker StartedAt 为起点，以 /readyz 首次成功为终点；
  # 10 秒只用于防止脚本挂死，正式是否通过仍由 LIMIT_MS 判断。
  started_at="$(
    docker inspect \
      --format '{{.State.StartedAt}}' \
      "${container_id}"
  )"
  started_ms="$(date --date="${started_at}" +%s%3N)"

  ready=0
  ready_at=""
  ready_ms=0
  deadline_ms="$((started_ms + 10000))"

  while [[ "$(date +%s%3N)" -le "${deadline_ms}" ]]; do
    if curl \
      --noproxy '*' \
      --silent \
      --fail \
      --output /dev/null \
      "http://127.0.0.1:${PUBLIC_PORT}/readyz"; then
      ready=1
      ready_at="$(date -u +%Y-%m-%dT%H:%M:%S.%3NZ)"
      ready_ms="$(date +%s%3N)"
      break
    fi

    sleep 0.02
  done

  if [[ "${ready}" == "1" ]]; then
    duration_ms="$((ready_ms - started_ms))"

    if [[ "${duration_ms}" -le "${LIMIT_MS}" ]]; then
      status="PASS"
    else
      status="FAIL"
      failed=1
    fi
  else
    duration_ms="-1"
    ready_at=""
    status="FAIL"
    failed=1
    docker logs "${container_id}" >&2 || true
  fi

  if [[ "${status}" == "PASS" && "${baseline}" != "1" ]]; then
    status="EXPLORATORY"
  fi

  printf '%s,%s,%s,%s,%s\n' \
    "${run_number}" \
    "${started_at}" \
    "${ready_at}" \
    "${duration_ms}" \
    "${status}" \
    | tee -a "${result_file}"

  docker rm --force "${container_id}" >/dev/null
  container_id=""
done

printf 'startup result: %s\n' "${result_file}"
printf 'startup environment: %s\n' "${environment_file}"

if [[ "${failed}" != "0" || "${baseline}" != "1" ]]; then
  exit 1
fi
