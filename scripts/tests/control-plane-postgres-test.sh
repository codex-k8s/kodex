#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Control-plane PostgreSQL test failed: %s\n' "$*" >&2
  exit 1
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
container_name="kodex-control-plane-postgres-${BASHPID}"
test_pattern=${1:-${KODEX_CONTROL_PLANE_TEST_FILTER:-'^(TestBootstrapComponent|TestWorkerGrantInstancesComponent|TestTrustedWorkloadGenerationComponent|TestAssistantCredentialScopesComponent|TestAssistantParallelLifecycleComponent|TestProjectAssistantProfilesComponent|TestOrganizationRoleImagesComponent|TestOrganizationRuntimeEnvironmentComponent|TestOrganizationRuntimeSecretComponent|TestAssistantSystemRunProjectScopeComponent)$'}}

cleanup() {
  docker stop --time 5 "$container_name" >/dev/null 2>&1 || true
}
trap cleanup EXIT

command -v docker >/dev/null 2>&1 || fail 'docker is required'
command -v pg_isready >/dev/null 2>&1 || fail 'pg_isready is required'
command -v psql >/dev/null 2>&1 || fail 'psql is required'

# Host-вариант нужен на локальных Linux-стендах, где Docker DNAT закрыт
# политикой хоста. PostgreSQL слушает только loopback; firewall не изменяется.
network_mode=${KODEX_TEST_POSTGRES_NETWORK:-bridge}
network_arguments=(-p 127.0.0.1::5432)
postgres_arguments=()
case "$network_mode" in
  bridge) ;;
  host)
    command -v node >/dev/null 2>&1 || fail 'node is required for host network fixture'
    [[ ( -z "${DOCKER_HOST:-}" || "$DOCKER_HOST" == unix:///* ) &&
       "$(docker context inspect --format '{{.Endpoints.docker.Host}}')" == unix:///* &&
       "$(docker info --format '{{.OSType}}')" == linux ]] || fail 'host fixture requires local Linux Docker'
    port=$(node --input-type=module -e '
      import {createServer} from "node:net";
      const server = createServer();
      server.on("error", () => process.exit(1));
      server.listen(0, "127.0.0.1", () => {
        process.stdout.write(String(server.address().port));
        server.close();
      });
    ') || fail 'host fixture port allocation failed'
    [[ "$port" =~ ^[0-9]+$ && "$port" -ge 1024 && "$port" -le 65535 ]] || fail 'host fixture port is invalid'
    network_arguments=(--network host)
    postgres_arguments=(postgres -c listen_addresses=127.0.0.1 -p "$port")
    ;;
  *) fail 'test PostgreSQL network must be bridge or host' ;;
esac

# У rootless Docker выбранный автоматически порт иногда успевает занять другой
# локальный процесс до того, как RootlessKit создаст listener. Повторяем только
# этот отказ до запуска теста; уже созданную БД и тестовые эффекты не повторяем.
for attempt in 1 2 3 4; do
  if docker_run_output=$(docker run --rm -d --name "$container_name" \
    -e POSTGRES_HOST_AUTH_METHOD=trust \
    "${network_arguments[@]}" \
    docker.io/library/postgres:18.3-alpine3.23@sha256:54451ecb8ab38c24c3ec123f2fd501303a3a1856a5c66e98cecf2460d5e1e9d7 \
    "${postgres_arguments[@]}" \
    2>&1); then
    break
  fi
  if [[ "$docker_run_output" != *"RootlessKit PortManager.AddPort()"* ||
        "$docker_run_output" != *"bind: address already in use"* ]]; then
    fail "docker run: $docker_run_output"
  fi
  if (( attempt == 4 )); then
    fail 'disposable PostgreSQL port allocation exhausted'
  fi
  sleep 1
done

if [[ "$network_mode" == bridge ]]; then
  port=$(docker inspect --format '{{(index (index .NetworkSettings.Ports "5432/tcp") 0).HostPort}}' "$container_name")
fi
[[ "$port" =~ ^[0-9]+$ ]] || fail 'disposable PostgreSQL port is invalid'
for _ in $(seq 1 30); do
  if pg_isready -h 127.0.0.1 -p "$port" -U postgres -t 1 >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
pg_isready -h 127.0.0.1 -p "$port" -U postgres -t 1 >/dev/null 2>&1 ||
  fail 'disposable PostgreSQL did not become ready'

admin_dsn="postgresql://postgres@127.0.0.1:${port}/postgres?sslmode=disable"
psql "$admin_dsn" --no-password -v ON_ERROR_STOP=1 --file \
  "$repository_root/deploy/k8s/base/platform-state/postgresql/10-bootstrap.sql" \
  >/dev/null
dsn="postgresql://control_plane_migrator@127.0.0.1:${port}/control_plane?sslmode=disable"
runtime_dsn="postgresql://control_plane_runtime_g1@127.0.0.1:${port}/control_plane?sslmode=disable"
run_migration() {
  CONTROL_PLANE_POSTGRES_ADMIN_DSN_FILE=<(printf '%s' "$dsn") \
    env -u GOFLAGS GOENV=off GOWORK=off go run -p 2 ./cmd/cli "$@"
}

(
  cd -- "$repository_root/services/internal/control-plane"
  if [[ -n "${KODEX_CONTROL_PLANE_TEST_FILTER:-}" ]]; then
    psql "$admin_dsn" --no-password -v ON_ERROR_STOP=1 \
      -c 'CREATE DATABASE control_plane_scheduler_upgrade WITH TEMPLATE control_plane OWNER control_plane_owner' >/dev/null
    # Копируем ещё пустую canonical bootstrap-БД: Goose table/sequence принадлежат
    # owner, migrator получает только точные grants. NOINHERIT не заменяет это.
    psql "postgresql://postgres@127.0.0.1:${port}/control_plane_scheduler_upgrade?sslmode=disable" --no-password -v ON_ERROR_STOP=1 \
      -c 'REVOKE CONNECT ON DATABASE control_plane_scheduler_upgrade FROM PUBLIC; GRANT CONNECT, TEMPORARY ON DATABASE control_plane_scheduler_upgrade TO control_plane_migrator; REVOKE CREATE ON SCHEMA public FROM PUBLIC; GRANT USAGE, CREATE ON SCHEMA public TO control_plane_owner, control_plane_migrator' >/dev/null
    KODEX_CONTROL_PLANE_MIGRATION_TEST_DSN="postgresql://control_plane_migrator@127.0.0.1:${port}/control_plane_scheduler_upgrade?sslmode=disable" \
      KODEX_CONTROL_PLANE_MIGRATION_TEST_DISPOSABLE=dedicated-loopback-container \
      env -u GOFLAGS GOENV=off GOWORK=off go test -p 2 -count=1 -timeout=90s ./cmd/cli -run '^TestScheduleProtocolUpgrade$'
  fi
  run_migration up
  run_migration status >/dev/null
  run_migration up
  retention_reference_count=$(psql "$runtime_dsn" --no-password -X -qAt -v ON_ERROR_STOP=1 \
    -c "SELECT control_plane.skill_artifact_reference_count('00000000-0000-4000-8000-000000000001'::uuid,'art_component',1,'sha256:' || repeat('0',64))")
  [[ "$retention_reference_count" == "0" ]] || fail 'artifact retention trigger dependency is unavailable'
  # Новые самостоятельные suites получают отдельную копию ещё пустой схемы,
  # чтобы terminal fixtures одного сценария не загрязняли bootstrap другого.
  # Эти координаты существуют только внутри disposable контейнера этой оснастки.
  psql "$admin_dsn" --no-password -v ON_ERROR_STOP=1 \
    -c 'CREATE DATABASE control_plane_component_template WITH TEMPLATE control_plane OWNER control_plane_owner' >/dev/null
  KODEX_CONTROL_PLANE_TEST_DSN="$runtime_dsn" \
    KODEX_CONTROL_PLANE_TEST_ADMIN_DSN="$admin_dsn" \
    KODEX_CONTROL_PLANE_TEST_TEMPLATE_DATABASE=control_plane_component_template \
    env -u GOFLAGS GOENV=off GOWORK=off go test -p 2 -v -count=1 \
      ./internal/repository/postgres/platform -run "$test_pattern"
  # Та же read-only диагностика, которую release использует на staging.
  psql "postgresql://postgres@127.0.0.1:${port}/control_plane?sslmode=disable" \
    --no-password -X -qAt -v ON_ERROR_STOP=1 \
    --file "$repository_root/tools/release/worker-grant-readback.sql" | \
    node --input-type=module -e '
      let input = "";
      for await (const chunk of process.stdin) input += chunk;
      const state = JSON.parse(input);
      if (!Number.isFinite(Date.parse(state.at)) || !Array.isArray(state.floors) || !Array.isArray(state.instances))
        throw new Error("Worker grant readback shape is invalid");
      process.stdout.write("Worker grant read-only query passed\n");
    '
  psql "postgresql://postgres@127.0.0.1:${port}/control_plane?sslmode=disable" \
    --no-password -X -qAt -v ON_ERROR_STOP=1 \
    --file "$repository_root/tools/release/runner-policy-readback.sql" | \
    node --input-type=module -e '
      let input = "";
      for await (const chunk of process.stdin) input += chunk;
      const state = JSON.parse(input);
      if (!Number.isFinite(Date.parse(state.at)) ||
          !["openBuilds", "pendingAdmissions", "pendingPromotions", "activeRuntimeRuns", "claimedRuntimeLeases", "promotedArtifactCount"]
            .every((key) => Number.isSafeInteger(state[key]) && state[key] >= 0) ||
          !/^[a-f0-9]{64}$/.test(state.promotedPinsSHA256))
        throw new Error("Runner policy readback shape is invalid");
      process.stdout.write("Runner policy read-only query passed\n");
    '
  if [[ $# -eq 0 && -z "${KODEX_CONTROL_PLANE_TEST_FILTER:-}" ]]; then
    KODEX_CONTROL_PLANE_TEST_DSN="$runtime_dsn" \
      env -u GOFLAGS GOENV=off GOWORK=off go test -p 2 -count=1 \
        ./internal/repository/postgres/platform -run '^TestAvatarLifecycleComponent$'
  fi
)

printf 'Control-plane PostgreSQL tests passed\n'
