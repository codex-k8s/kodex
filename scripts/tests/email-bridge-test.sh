#!/usr/bin/env bash
set -euo pipefail
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
fail() {
  printf 'Email bridge component test failed: %s\n' "$*" >&2
  exit 1
}
storage_mode=${KODEX_TEST_POSTGRES_STORAGE:-disk}
[[ "$storage_mode" == disk || "$storage_mode" == ram ]] || fail 'test PostgreSQL storage must be disk or ram'
go_parallelism=${KODEX_EMAIL_BRIDGE_TEST_GO_PARALLELISM:-2}
[[ "$go_parallelism" =~ ^[12]$ ]] || fail 'Go parallelism must be 1 or 2'
if (( $# > 1 )); then
  printf 'Usage: %s [test-regexp]\n' "$0" >&2
  exit 2
fi
test_filter=${1:-.}
cd -- "$root/services/internal/email-bridge"
selected=$(env -u GOFLAGS GOENV=off GOWORK=off go test -p "$go_parallelism" -list "$test_filter" ./internal/component)
if ! printf '%s\n' "$selected" | rg -q '^Test'; then
  printf 'No email bridge component tests selected\n' >&2
  exit 2
fi
name="kodex-email-bridge-test-${BASHPID}"
container_id=""
temporary=$(mktemp -d)
cleanup() {
  if [[ "$container_id" =~ ^[a-f0-9]{64}$ ]]; then
    docker stop --time 5 "$container_id" >/dev/null 2>&1 || true
  fi
  rm -rf -- "$temporary"
}
trap cleanup EXIT
postgres_image='docker.io/library/postgres:18.3-alpine3.23@sha256:54451ecb8ab38c24c3ec123f2fd501303a3a1856a5c66e98cecf2460d5e1e9d7'
storage_arguments=()
postgres_arguments=()
if [[ "$storage_mode" == ram ]]; then
  command -v jq >/dev/null 2>&1 || fail 'jq is required for RAM fixture readback'
  [[ ( -z "${DOCKER_HOST:-}" || "$DOCKER_HOST" == unix:///* ) &&
     "$(docker context inspect --format '{{.Endpoints.docker.Host}}')" == unix:///* &&
     "$(docker info --format '{{.OSType}}')" == linux ]] || fail 'RAM fixture requires local Linux Docker'
  cached_image_id=$(docker image inspect --format '{{.Id}}' "$postgres_image") || fail 'cached PostgreSQL image is required for RAM fixture'
  [[ "$cached_image_id" =~ ^sha256:[a-f0-9]{64}$ ]] || fail 'cached PostgreSQL identity is invalid'
  # Те же реальные limits/mounts, что у control-plane fixture: ни pull, ни
  # anonymous volume на диске. После create граница проверяется по ACK ID.
  storage_arguments=(--pull never --read-only --memory 1073741824 --memory-swap 1073741824
    --cpus 1 --pids-limit 128 --log-driver none
    --tmpfs '/var/lib/postgresql:rw,noexec,nosuid,nodev,size=536870912,mode=0700,uid=70,gid=70'
    --tmpfs '/var/run/postgresql:rw,noexec,nosuid,nodev,size=16777216,mode=0775'
    --tmpfs '/tmp:rw,noexec,nosuid,nodev,size=16777216,mode=1777')
  postgres_arguments=(postgres -c shared_buffers=32MB -c max_wal_size=64MB -c min_wal_size=32MB -c checkpoint_timeout=30s)
fi
container_id=$(docker run --rm -d --name "$name" "${storage_arguments[@]}" -e POSTGRES_HOST_AUTH_METHOD=trust \
  -p 127.0.0.1::5432 \
  "$postgres_image" "${postgres_arguments[@]}")
[[ "$container_id" =~ ^[a-f0-9]{64}$ ]] || fail 'disposable container identity is unconfirmed'
if [[ "$storage_mode" == ram ]]; then
  memory_max=$(docker exec "$container_id" cat /sys/fs/cgroup/memory.max)
  cpu_max=$(docker exec "$container_id" cat /sys/fs/cgroup/cpu.max)
  filesystem=$(docker exec "$container_id" stat -f -c %T /var/lib/postgresql)
  data_owner=$(docker exec "$container_id" stat -c %u:%g:%a /var/lib/postgresql)
  [[ "$memory_max" == 1073741824 && "$cpu_max" == '100000 100000' && "$filesystem" == tmpfs && "$data_owner" == 70:70:700 ]] || fail 'RAM fixture effective boundary differs'
  boundary=$(docker inspect --format '{"id":{{json .Id}},"image":{{json .Image}},"readonly":{{json .HostConfig.ReadonlyRootfs}},"memory":{{json .HostConfig.Memory}},"swap":{{json .HostConfig.MemorySwap}},"cpu":{{json .HostConfig.NanoCpus}},"pids":{{json .HostConfig.PidsLimit}},"tmpfs":{{json .HostConfig.Tmpfs}},"mounts":{{json .Mounts}}}' "$container_id")
  jq -e --arg id "$container_id" --arg image "$cached_image_id" '
    .id==$id and .image==$image and .readonly==true and
    .memory==1073741824 and .swap==1073741824 and .cpu==1000000000 and .pids==128 and
    (.tmpfs|keys==["/tmp","/var/lib/postgresql","/var/run/postgresql"]) and
    .tmpfs["/var/lib/postgresql"]=="rw,noexec,nosuid,nodev,size=536870912,mode=0700,uid=70,gid=70" and
    .tmpfs["/var/run/postgresql"]=="rw,noexec,nosuid,nodev,size=16777216,mode=0775" and
    .tmpfs["/tmp"]=="rw,noexec,nosuid,nodev,size=16777216,mode=1777" and
    all(.mounts[];.Type=="tmpfs")
  ' <<<"$boundary" >/dev/null || fail 'RAM fixture mount boundary differs'
  printf 'Disposable PostgreSQL RAM boundary verified\n'
fi
port=$(docker inspect --format '{{(index (index .NetworkSettings.Ports "5432/tcp") 0).HostPort}}' "$container_id")
[[ "$port" =~ ^[0-9]+$ ]]
for _ in $(seq 1 30); do
  if pg_isready -h 127.0.0.1 -p "$port" -U postgres >/dev/null 2>&1; then break; fi
  sleep 1
done
pg_isready -h 127.0.0.1 -p "$port" -U postgres >/dev/null
psql "postgresql://postgres@127.0.0.1:${port}/postgres?sslmode=disable" -v ON_ERROR_STOP=1 \
  -c 'CREATE ROLE email_bridge_runtime LOGIN NOSUPERUSER NOBYPASSRLS; CREATE ROLE email_bridge_migrator LOGIN NOSUPERUSER NOBYPASSRLS;' >/dev/null
psql "postgresql://postgres@127.0.0.1:${port}/postgres?sslmode=disable" -v ON_ERROR_STOP=1 \
  -c 'CREATE DATABASE email_bridge OWNER email_bridge_migrator;' >/dev/null
printf 'postgresql://email_bridge_migrator@127.0.0.1:%s/email_bridge?sslmode=disable' "$port" > "$temporary/migration-dsn"
chmod 0400 "$temporary/migration-dsn"
export EMAIL_BRIDGE_MIGRATION_DSN_FILE="$temporary/migration-dsn"
env -u GOFLAGS GOENV=off GOWORK=off go run -p "$go_parallelism" ./cmd/cli up
env -u GOFLAGS GOENV=off GOWORK=off go run -p "$go_parallelism" ./cmd/cli status
env -u GOFLAGS GOENV=off GOWORK=off go run -p "$go_parallelism" ./cmd/cli up
EMAIL_BRIDGE_TEST_DSN="postgresql://email_bridge_runtime@127.0.0.1:${port}/email_bridge?sslmode=disable" \
  EMAIL_BRIDGE_TEST_ADMIN_DSN="postgresql://postgres@127.0.0.1:${port}/postgres?sslmode=disable" \
  env -u GOFLAGS GOENV=off GOWORK=off go test -race -p "$go_parallelism" -count=1 -timeout=90s -v ./internal/component -run "$test_filter"
printf 'Email bridge PostgreSQL and protocol component tests passed\n'
