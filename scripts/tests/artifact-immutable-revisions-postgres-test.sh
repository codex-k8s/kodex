#!/usr/bin/env bash
set -euo pipefail

# Изолированная проверка upgrade и настоящего retention adapter. Только local
# Docker, loopback PostgreSQL18, синтетические metadata и object storage mock.
repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
container_name="kodex-artifact-revisions-${BASHPID}"
cleanup() { docker stop --time 5 "$container_name" >/dev/null 2>&1 || true; }
trap cleanup EXIT
for executable in docker psql pg_isready node go; do command -v "$executable" >/dev/null; done
[[ ( -z "${DOCKER_HOST:-}" || "$DOCKER_HOST" == unix:///* ) &&
   "$(docker context inspect --format '{{.Endpoints.docker.Host}}')" == unix:///* &&
   "$(docker info --format '{{.OSType}}')" == linux ]] || {
  printf 'Artifact fixture requires local Linux Docker\n' >&2; exit 1;
}
port=$(node --input-type=module -e '
 import {createServer} from "node:net";
 const server=createServer();server.on("error",()=>process.exit(1));
 server.listen(0,"127.0.0.1",()=>{process.stdout.write(String(server.address().port));server.close();});
')
[[ "$port" =~ ^[0-9]+$ && "$port" -ge 1024 && "$port" -le 65535 ]]
docker run --rm -d --name "$container_name" --network host -e POSTGRES_HOST_AUTH_METHOD=trust \
 docker.io/library/postgres:18.3-alpine3.23@sha256:54451ecb8ab38c24c3ec123f2fd501303a3a1856a5c66e98cecf2460d5e1e9d7 \
 postgres -c listen_addresses=127.0.0.1 -p "$port" >/dev/null
for _ in $(seq 1 30); do
 if pg_isready -h 127.0.0.1 -p "$port" -U postgres -t 1 >/dev/null 2>&1; then break; fi
 sleep 1
done
pg_isready -h 127.0.0.1 -p "$port" -U postgres -t 1 >/dev/null
admin_dsn="postgresql://postgres@127.0.0.1:${port}/postgres?sslmode=disable"
psql "$admin_dsn" --no-password -v ON_ERROR_STOP=1 --file \
 "$repository_root/deploy/k8s/base/platform-state/postgresql/10-bootstrap.sql" >/dev/null
psql "$admin_dsn" --no-password -v ON_ERROR_STOP=1 \
 -c 'CREATE DATABASE control_plane_artifact_revision_upgrade WITH TEMPLATE control_plane OWNER control_plane_owner' >/dev/null
upgrade_dsn="postgresql://control_plane_migrator@127.0.0.1:${port}/control_plane_artifact_revision_upgrade?sslmode=disable"
psql "postgresql://postgres@127.0.0.1:${port}/control_plane_artifact_revision_upgrade?sslmode=disable" --no-password -v ON_ERROR_STOP=1 \
 -c 'REVOKE CONNECT ON DATABASE control_plane_artifact_revision_upgrade FROM PUBLIC; GRANT CONNECT,TEMPORARY ON DATABASE control_plane_artifact_revision_upgrade TO control_plane_migrator; REVOKE CREATE ON SCHEMA public FROM PUBLIC; GRANT USAGE,CREATE ON SCHEMA public TO control_plane_owner,control_plane_migrator' >/dev/null
(
 cd -- "$repository_root/services/internal/control-plane"
 KODEX_ARTIFACT_REVISION_UPGRADE_DSN="$upgrade_dsn" \
 KODEX_ARTIFACT_REVISION_DISPOSABLE=dedicated-loopback-container \
 env -u GOFLAGS GOENV=off GOWORK=off go test -p 2 -v -count=1 -timeout=120s ./cmd/cli -run '^TestImmutableArtifactProtocolUpgrade$'
 migrator_dsn="postgresql://control_plane_migrator@127.0.0.1:${port}/control_plane?sslmode=disable"
 CONTROL_PLANE_POSTGRES_ADMIN_DSN_FILE=<(printf '%s' "$migrator_dsn") \
 env -u GOFLAGS GOENV=off GOWORK=off go run -p 2 ./cmd/cli up
)
(
 cd -- "$repository_root/services/jobs/artifact-retention"
 KODEX_ARTIFACT_RETENTION_COMPONENT_DSN="postgresql://artifact_retention_runtime_g1@127.0.0.1:${port}/control_plane?sslmode=disable" \
 KODEX_ARTIFACT_RETENTION_COMPONENT_ADMIN_DSN="postgresql://postgres@127.0.0.1:${port}/control_plane?sslmode=disable" \
 KODEX_ARTIFACT_REVISION_DISPOSABLE=dedicated-loopback-container \
 env -u GOFLAGS GOENV=off GOWORK=off go test -p 2 -v -count=1 -timeout=60s ./internal/repository/postgres -run '^TestRevisionRetentionComponent$'
)
printf 'Artifact immutable revision upgrade and retention tests passed\n'
