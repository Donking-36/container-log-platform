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
readonly IMAGE="${PERF_IMAGE:-container-log-platform-api:1.0.0}"
readonly PUBLIC_PORT="${PERF_PUBLIC_PORT:-18080}"
readonly INTERNAL_PORT="${PERF_INTERNAL_PORT:-18081}"
readonly RECORDS="${PERF_RECORDS:-10000}"
readonly CONCURRENCY="${PERF_CONCURRENCY:-10}"
readonly DURATION="${PERF_DURATION:-30s}"
readonly RUN_ID="${PERF_RUN_ID:-$(date -u +%Y%m%dT%H%M%SZ)-$$}"
readonly RESULT_DIR="${PERF_RESULT_DIR:-${REPO_ROOT}/artifacts/acceptance}"

data_dir=""
container_id=""

cleanup() {
  if [[ -n "${container_id}" ]]; then
    docker rm --force "${container_id}" >/dev/null 2>&1 || true
  fi

  if [[ -n "${data_dir}" ]]; then
    case "${data_dir}" in
      /tmp/container-log-platform-performance.*)
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

for command_name in docker go curl; do
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    printf 'required command not found: %s\n' "${command_name}" >&2
    exit 1
  fi
done

if ! [[ "${RUN_ID}" =~ ^[A-Za-z0-9._-]+$ ]]; then
  printf 'PERF_RUN_ID may contain only letters, digits, dot, underscore and hyphen\n' \
    >&2
  exit 1
fi

mkdir -p "${RESULT_DIR}"

if [[ "${PERF_SKIP_BUILD:-0}" != "1" ]]; then
  docker compose build api
fi

data_dir="$(mktemp -d /tmp/container-log-platform-performance.XXXXXX)"

# 性能验收临时发布内部 8081 端口，仅用于确定性批量造数；
# 正常 Compose 从不向宿主机暴露该写入接口。
container_id="$(
  docker run \
  --detach \
  --publish "127.0.0.1:${PUBLIC_PORT}:8080" \
  --publish "127.0.0.1:${INTERNAL_PORT}:8081" \
  --env APP_ENV=production \
  --env DATABASE_PATH=/app/data/performance.db \
  --volume "${data_dir}:/app/data" \
  "${IMAGE}" \
)"

readonly public_url="http://127.0.0.1:${PUBLIC_PORT}"
readonly internal_url="$(
  printf 'http://127.0.0.1:%s/internal/v1/logs/bulk' \
    "${INTERNAL_PORT}"
)"

ready=0
for _ in $(seq 1 200); do
  if curl \
    --noproxy '*' \
    --silent \
    --fail \
    --output /dev/null \
    "${public_url}/readyz"; then
    ready=1
    break
  fi

  sleep 0.05
done

if [[ "${ready}" != "1" ]]; then
  docker logs "${container_id}" >&2 || true
  printf 'performance API did not become ready\n' >&2
  exit 1
fi

# 保存代码版本、机器和容器运行时快照，使性能结果可以复现和解释。
environment_file="${RESULT_DIR}/${RUN_ID}-environment.txt"
result_file="${RESULT_DIR}/${RUN_ID}-query-performance.json"

{
  printf 'run_id=%s\n' "${RUN_ID}"
  printf 'git_commit=%s\n' "$(git rev-parse HEAD)"
  printf 'test_command=PERF_RUN_ID=%s ./scripts/acceptance/query-performance.sh\n' \
    "${RUN_ID}"
  printf 'records=%s\n' "${RECORDS}"
  printf 'concurrency=%s\n' "${CONCURRENCY}"
  printf 'duration=%s\n' "${DURATION}"
  printf 'image=%s\n' "${IMAGE}"
  printf 'wsl_kernel=%s\n' "$(uname -r)"
  uname -a
  grep -E '^(model name|processor|MemTotal)' \
    /proc/cpuinfo \
    /proc/meminfo \
    2>/dev/null \
    | head -n 4
  go version
  docker version
  docker compose version
} | tee "${environment_file}"

benchmark_args=()
if [[ "${PERF_ALLOW_NON_BASELINE:-0}" == "1" ]]; then
  # 非基线参数只用于探索；Go 基准程序不会把结果标记为正式验收通过。
  benchmark_args+=("-allow-non-baseline")
fi

error_file="${RESULT_DIR}/${RUN_ID}-query-performance.stderr.log"

go run ./test/performance \
  -internal-url "${internal_url}" \
  -public-url "${public_url}" \
  -records "${RECORDS}" \
  -concurrency "${CONCURRENCY}" \
  -duration "${DURATION}" \
  "${benchmark_args[@]}" \
  2> >(tee "${error_file}" >&2) \
  | tee "${result_file}"

printf 'query performance result: %s\n' "${result_file}"
printf 'test environment: %s\n' "${environment_file}"
