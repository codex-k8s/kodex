#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'Impact owner snapshot upgrade test failed: %s\n' "$1" >&2; exit 1; }
repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
container_name="kodex-impact-owner-upgrade-${BASHPID}"
fixture_instance=""
cleanup() {
  # Не останавливаем одноимённый чужой контейнер после отказа docker run.
  if [[ -n "$fixture_instance" &&
    "$(timeout 5s docker inspect --format '{{index .Config.Labels "kodex.dev/test-instance"}}' "$container_name" 2>/dev/null || true)" == "$fixture_instance" ]]; then
    timeout 15s docker stop --time 5 "$container_name" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

# Не принимаем координаты общей БД или credentials вызывающей среды.
for key in KODEX_IMPACT_OWNER_SNAPSHOT_UPGRADE_DSN KODEX_IMPACT_OWNER_SNAPSHOT_UPGRADE_DISPOSABLE \
  KODEX_CONTROL_PLANE_TEST_DSN KODEX_CONTROL_PLANE_MIGRATION_TEST_DSN; do
  [[ ! -v "$key" ]] || fail "external fixture configuration is forbidden: $key"
done
for command in docker node psql pg_isready timeout go; do command -v "$command" >/dev/null || fail "$command is required"; done
[[ ( -z "${DOCKER_HOST:-}" || "$DOCKER_HOST" == unix:///* ) &&
  "$(docker context inspect --format '{{.Endpoints.docker.Host}}')" == unix:///* &&
  "$(docker info --format '{{.OSType}}')" == linux ]] || fail 'local Linux Docker is required'
fixture_instance=$(node --input-type=module -e 'import {randomUUID} from "node:crypto";process.stdout.write(randomUUID());')

port=$(node --input-type=module -e '
import {createServer} from "node:net";
const server=createServer();
server.on("error",()=>process.exit(1));
server.listen(0,"127.0.0.1",()=>{process.stdout.write(String(server.address().port));server.close();});
')
[[ "$port" =~ ^[0-9]+$ && "$port" -ge 1024 && "$port" -le 65535 ]] || fail 'loopback port allocation failed'
timeout 60s docker run --rm -d --name "$container_name" --network host \
  --label "kodex.dev/test-instance=$fixture_instance" \
  -e POSTGRES_HOST_AUTH_METHOD=trust \
  docker.io/library/postgres:18.3-alpine3.23@sha256:54451ecb8ab38c24c3ec123f2fd501303a3a1856a5c66e98cecf2460d5e1e9d7 \
  postgres -c listen_addresses=127.0.0.1 -p "$port" >/dev/null
for ((attempt=0; attempt<30; attempt++)); do
  if pg_isready -h 127.0.0.1 -p "$port" -U postgres -t 1 >/dev/null 2>&1; then break; fi
  sleep 1
done
pg_isready -h 127.0.0.1 -p "$port" -U postgres -t 1 >/dev/null 2>&1 || fail 'disposable PostgreSQL is unavailable'

# DSN только синтетические, назначаются этой оснасткой после запуска контейнера.
admin_dsn="postgresql://postgres@127.0.0.1:${port}/postgres?sslmode=disable"
env -u PGPASSWORD -u PGSERVICE -u PGSERVICEFILE -u PGOPTIONS -u PGHOST -u PGPORT -u PGUSER -u PGDATABASE \
  PGPASSFILE=/dev/null \
  timeout 30s psql "$admin_dsn" --no-password -X -v ON_ERROR_STOP=1 \
  --file "$repository_root/deploy/k8s/base/platform-state/postgresql/10-bootstrap.sql" >/dev/null
env -u PGPASSWORD -u PGSERVICE -u PGSERVICEFILE -u PGOPTIONS \
  PGPASSFILE=/dev/null \
  timeout 15s psql "$admin_dsn" --no-password -X -v ON_ERROR_STOP=1 \
  -c 'CREATE DATABASE control_plane_impact_upgrade WITH TEMPLATE control_plane OWNER control_plane_owner' >/dev/null
fixture_dsn="postgresql://postgres@127.0.0.1:${port}/control_plane_impact_upgrade?sslmode=disable"
env -u PGPASSWORD -u PGSERVICE -u PGSERVICEFILE -u PGOPTIONS \
  PGPASSFILE=/dev/null \
  timeout 15s psql "$fixture_dsn" --no-password -X -v ON_ERROR_STOP=1 \
  -c 'REVOKE CONNECT ON DATABASE control_plane_impact_upgrade FROM PUBLIC; GRANT CONNECT, TEMPORARY ON DATABASE control_plane_impact_upgrade TO control_plane_migrator; REVOKE CREATE ON SCHEMA public FROM PUBLIC; GRANT USAGE, CREATE ON SCHEMA public TO control_plane_owner, control_plane_migrator' >/dev/null
(
  cd -- "$repository_root/services/internal/control-plane"
  env -u GOFLAGS -u PGPASSWORD -u PGSERVICE -u PGSERVICEFILE -u PGOPTIONS -u PGHOST -u PGPORT -u PGUSER -u PGDATABASE \
    PGPASSFILE=/dev/null \
    GOTOOLCHAIN=go1.26.6 GOMAXPROCS=2 GOENV=off GOWORK=off \
    KODEX_IMPACT_OWNER_SNAPSHOT_UPGRADE_DSN="postgresql://control_plane_migrator@127.0.0.1:${port}/control_plane_impact_upgrade?sslmode=disable" \
    KODEX_IMPACT_OWNER_SNAPSHOT_UPGRADE_DISPOSABLE=dedicated-loopback-container \
    timeout 180s go test -p 2 -count=1 -v -timeout=100s ./cmd/cli -run '^TestImpactOwnerSnapshotProtocolUpgrade$'
)
printf 'Impact owner snapshot forward upgrade passed; owner RPC acceptance is not verified\n'
