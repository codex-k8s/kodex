#!/usr/bin/env bash
set -euo pipefail
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
name="kodex-email-bridge-test-${BASHPID}"
temporary=$(mktemp -d)
cleanup() {
  docker stop --time 5 "$name" >/dev/null 2>&1 || true
  rm -rf -- "$temporary"
}
trap cleanup EXIT
docker run --rm -d --name "$name" -e POSTGRES_HOST_AUTH_METHOD=trust \
  -p 127.0.0.1::5432 \
  docker.io/library/postgres:18.3-alpine3.23@sha256:54451ecb8ab38c24c3ec123f2fd501303a3a1856a5c66e98cecf2460d5e1e9d7 >/dev/null
port=$(docker inspect --format '{{(index (index .NetworkSettings.Ports "5432/tcp") 0).HostPort}}' "$name")
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
cd -- "$root/services/internal/email-bridge"
export EMAIL_BRIDGE_MIGRATION_DSN_FILE="$temporary/migration-dsn"
env -u GOFLAGS GOENV=off GOWORK=off go run ./cmd/cli up
env -u GOFLAGS GOENV=off GOWORK=off go run ./cmd/cli status
env -u GOFLAGS GOENV=off GOWORK=off go run ./cmd/cli up
EMAIL_BRIDGE_TEST_DSN="postgresql://email_bridge_runtime@127.0.0.1:${port}/email_bridge?sslmode=disable" \
  env -u GOFLAGS GOENV=off GOWORK=off go test -race -count=1 -timeout=90s -v ./internal/component
printf 'Email bridge PostgreSQL and protocol component tests passed\n'
