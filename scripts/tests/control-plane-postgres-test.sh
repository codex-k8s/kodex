#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Control-plane PostgreSQL test failed: %s\n' "$*" >&2
  exit 1
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
container_name="mattercodex-control-plane-postgres-${BASHPID}"

cleanup() {
  docker stop --time 5 "$container_name" >/dev/null 2>&1 || true
}
trap cleanup EXIT

command -v docker >/dev/null 2>&1 || fail 'docker is required'
command -v pg_isready >/dev/null 2>&1 || fail 'pg_isready is required'

docker run --rm -d --name "$container_name" \
  -e POSTGRES_HOST_AUTH_METHOD=trust \
  -p 127.0.0.1::5432 \
  docker.io/library/postgres:18.3-alpine3.23@sha256:54451ecb8ab38c24c3ec123f2fd501303a3a1856a5c66e98cecf2460d5e1e9d7 \
  >/dev/null

port=$(docker inspect --format '{{(index (index .NetworkSettings.Ports "5432/tcp") 0).HostPort}}' "$container_name")
[[ "$port" =~ ^[0-9]+$ ]] || fail 'disposable PostgreSQL port is invalid'
for _ in $(seq 1 30); do
  if pg_isready -h 127.0.0.1 -p "$port" -U postgres >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
pg_isready -h 127.0.0.1 -p "$port" -U postgres >/dev/null 2>&1 ||
  fail 'disposable PostgreSQL did not become ready'

dsn="postgresql://postgres@127.0.0.1:${port}/postgres?sslmode=disable"
run_migration() {
  CONTROL_PLANE_POSTGRES_ADMIN_DSN_FILE=<(printf '%s' "$dsn") \
    env -u GOFLAGS GOENV=off GOWORK=off go run ./cmd/cli "$@"
}

(
  cd -- "$repository_root/services/internal/control-plane"
  run_migration up
  run_migration status >/dev/null
  run_migration up
  MATTERCODEX_CONTROL_PLANE_TEST_DSN="$dsn" \
    env -u GOFLAGS GOENV=off GOWORK=off go test -count=1 \
      ./internal/repository/postgres/platform -run '^TestBootstrapComponent$'
)

printf 'Control-plane PostgreSQL tests passed\n'
