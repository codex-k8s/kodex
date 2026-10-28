#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Control-plane PostgreSQL test failed: %s\n' "$*" >&2
  exit 1
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
container_name="kodex-control-plane-postgres-${BASHPID}"
go_parallelism=${KODEX_CONTROL_PLANE_TEST_GO_PARALLELISM:-2}
[[ "$go_parallelism" =~ ^[12]$ ]] || fail 'Go parallelism must be 1 or 2'
storage_mode=${KODEX_TEST_POSTGRES_STORAGE:-disk}
[[ "$storage_mode" == disk || "$storage_mode" == ram ]] || fail 'test PostgreSQL storage must be disk or ram'
postgres_image='docker.io/library/postgres:18.3-alpine3.23@sha256:54451ecb8ab38c24c3ec123f2fd501303a3a1856a5c66e98cecf2460d5e1e9d7'
upgrade_container_id=""
test_pattern=${1:-${KODEX_CONTROL_PLANE_TEST_FILTER:-'^(TestBootstrapComponent|TestConcurrentBootstrapComponent|TestWorkerGrantInstancesComponent|TestTrustedWorkloadGenerationComponent|TestAssistantCredentialScopesComponent|TestAssistantParallelLifecycleComponent|TestProjectAssistantProfilesComponent|TestOrganizationRoleImagesComponent|TestOrganizationRuntimeEnvironmentComponent|TestOrganizationRuntimeSecretComponent|TestAssistantSystemRunProjectScopeComponent|TestRuntimeTerminalStorageComponent|TestIntegrationGateFeedbackComponent|TestWorkflowMetadataDraftComponent|TestDeletedEnvironmentSecretRetentionComponent|TestHistoricalEnvironmentSecretAdmissionComponent|TestHistoricalSecretRecoveryConcurrencyComponent|TestWorkflowGateDecisionsComponent|TestCompletionReceiptComponent|TestUsageCompletenessComponent|TestRepositoryBundlePublicationComponent|TestCommitAckArtifactComponent|TestOwnerGateExpiryComponent|TestOwnerGateServerCompletionComponent|TestOwnerGateExpiryMigrationProofComponent|TestProjectPurgeAuthorityComponent|TestProjectPurgeAuthorityMigrationProofComponent|TestServerWorkflowMigrationProofComponent|TestOverviewEligibilityComponent|TestWorkflowExceptionContextComponent|TestWorkflowExceptionContextMigrationProofComponent)$'}}

# Оба закрытых upgrade-сценария используют одну и ту же отдельную пустую БД.
upgrade_test='^TestWorkflowReviewQAEvidenceForwardUpgrade$'
volume_owner=false
if [[ "$test_pattern" == --restore-owner ]]; then
  volume_owner=true
  upgrade_test='^TestWorkspaceRestoreOwnerMigration$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --volume-owner ]]; then
  volume_owner=true
  upgrade_test='^TestWorkspaceVolumeMigrationPredecessor$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --volume-predecessor ]]; then
  upgrade_test='^TestWorkspaceVolumeMigrationPredecessor$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --checkpoint-predecessor ]]; then
  upgrade_test='^TestWorkspaceCheckpointMigrationPredecessor$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --workspace-predecessor ]]; then
  upgrade_test='^TestSessionWorkspaceMigrationPredecessor$'
  test_pattern=--review-qa-upgrade
fi

# Новый upgrade-контур принимает только созданную ниже пустую disposable БД.
if [[ "$test_pattern" == --review-qa-upgrade ]]; then
  for key in KODEX_WORKFLOW_REVIEW_QA_UPGRADE_DSN KODEX_WORKFLOW_REVIEW_QA_UPGRADE_DISPOSABLE; do
    [[ ! -v "$key" ]] || fail "external fixture configuration is forbidden: $key"
  done
  unset PGPASSWORD PGSERVICE PGSERVICEFILE PGOPTIONS PGHOST PGPORT PGUSER PGDATABASE
  export PGPASSFILE=/dev/null
fi
if [[ "$storage_mode" == ram ]]; then
  for key in KODEX_CONTROL_PLANE_TEST_DSN KODEX_CONTROL_PLANE_TEST_ADMIN_DSN; do
    [[ ! -v "$key" ]] || fail "external fixture configuration is forbidden: $key"
  done
  unset PGPASSWORD PGSERVICE PGSERVICEFILE PGOPTIONS PGHOST PGPORT PGUSER PGDATABASE
  export PGPASSFILE=/dev/null
fi

cleanup() {
  if [[ "$test_pattern" == --review-qa-upgrade || "$storage_mode" == ram ]]; then
    # Новый контур никогда не очищает лишь совпавшее имя после неизвестного create.
    if [[ "$upgrade_container_id" =~ ^[a-f0-9]{64}$ ]]; then
      docker stop --time 5 "$upgrade_container_id" >/dev/null 2>&1 || true
      # stop может вернуться до завершения AutoRemove; проверяем exact ID через
      # успешную авторитетную выборку, а не считаем ошибку inspect отсутствием.
      local cleanup_ids
      for _ in {1..40}; do
        cleanup_ids=$(docker ps -a --no-trunc --filter "id=$upgrade_container_id" --format '{{.ID}}') ||
          fail 'disposable PostgreSQL cleanup readback failed'
        [[ -n "$cleanup_ids" ]] || return 0
        [[ "$cleanup_ids" == "$upgrade_container_id" ]] || fail 'disposable PostgreSQL cleanup identity changed'
        sleep 0.25
      done
      fail 'disposable PostgreSQL cleanup absence is not confirmed'
    fi
    return
  fi
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

storage_arguments=()
if [[ "$storage_mode" == ram ]]; then
  command -v jq >/dev/null 2>&1 || fail 'jq is required for RAM fixture readback'
  [[ ( -z "${DOCKER_HOST:-}" || "$DOCKER_HOST" == unix:///* ) &&
     "$(docker context inspect --format '{{.Endpoints.docker.Host}}')" == unix:///* &&
     "$(docker info --format '{{.OSType}}')" == linux ]] || fail 'RAM fixture requires local Linux Docker'
  # Только уже установленный exact image; данные и WAL не попадают в anonymous volume.
  cached_image_id=$(docker image inspect --format '{{.Id}}' "$postgres_image") || fail 'cached PostgreSQL image is required for RAM fixture'
  [[ "$cached_image_id" =~ ^sha256:[a-f0-9]{64}$ ]] || fail 'cached PostgreSQL identity is invalid'
  storage_arguments=(--pull never --read-only --memory 1073741824 --memory-swap 1073741824
    --cpus 1 --pids-limit 128 --log-driver none
    --tmpfs '/var/lib/postgresql:rw,noexec,nosuid,nodev,size=536870912,mode=0700,uid=70,gid=70'
    --tmpfs '/var/run/postgresql:rw,noexec,nosuid,nodev,size=16777216,mode=0775'
    --tmpfs '/tmp:rw,noexec,nosuid,nodev,size=16777216,mode=1777')
  if (( ${#postgres_arguments[@]} == 0 )); then postgres_arguments=(postgres); fi
  postgres_arguments+=(-c shared_buffers=32MB -c max_wal_size=64MB -c min_wal_size=32MB -c checkpoint_timeout=30s)
fi

# У rootless Docker выбранный автоматически порт иногда успевает занять другой
# локальный процесс до того, как RootlessKit создаст listener. Повторяем только
# этот отказ до запуска теста; уже созданную БД и тестовые эффекты не повторяем.
for attempt in 1 2 3 4; do
  if docker_run_output=$(docker run --rm -d --name "$container_name" \
    -e POSTGRES_HOST_AUTH_METHOD=trust \
    "${network_arguments[@]}" \
    "${storage_arguments[@]}" \
    "$postgres_image" \
    "${postgres_arguments[@]}" \
    2>&1); then
    break
  fi
  if [[ "$storage_mode" == ram ]]; then
    # Не повторяем неизвестный Create и не очищаем контейнер лишь по совпавшему имени.
    fail 'RAM fixture start is unconfirmed; no retry performed'
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

if [[ "$test_pattern" == --review-qa-upgrade || "$storage_mode" == ram ]]; then
  [[ "$docker_run_output" =~ ^[a-f0-9]{64}$ ]] || fail 'disposable container identity is unconfirmed'
  upgrade_container_id="$docker_run_output"
fi

if [[ "$storage_mode" == ram ]]; then
  # Проверяем actual cgroup и mounts до bootstrap, не полагаясь лишь на argv Docker.
  memory_max=$(docker exec "$upgrade_container_id" cat /sys/fs/cgroup/memory.max) || fail 'RAM fixture memory readback failed'
  cpu_max=$(docker exec "$upgrade_container_id" cat /sys/fs/cgroup/cpu.max) || fail 'RAM fixture CPU readback failed'
  filesystem=$(docker exec "$upgrade_container_id" stat -f -c %T /var/lib/postgresql) || fail 'RAM fixture filesystem readback failed'
  data_owner=$(docker exec "$upgrade_container_id" stat -c %u:%g:%a /var/lib/postgresql) || fail 'RAM fixture data owner readback failed'
  [[ "$memory_max" == 1073741824 && "$cpu_max" == '100000 100000' && "$filesystem" == tmpfs && "$data_owner" == 70:70:700 ]] || fail 'RAM fixture effective boundary differs'
  boundary=$(docker inspect --format '{"id":{{json .Id}},"image":{{json .Image}},"readonly":{{json .HostConfig.ReadonlyRootfs}},"memory":{{json .HostConfig.Memory}},"swap":{{json .HostConfig.MemorySwap}},"cpu":{{json .HostConfig.NanoCpus}},"pids":{{json .HostConfig.PidsLimit}},"tmpfs":{{json .HostConfig.Tmpfs}},"mounts":{{json .Mounts}}}' "$upgrade_container_id") || fail 'RAM fixture mount readback failed'
  jq -e --arg id "$upgrade_container_id" --arg image "$cached_image_id" '
    .id == $id and .image == $image and .readonly == true and
    .memory == 1073741824 and .swap == 1073741824 and .cpu == 1000000000 and .pids == 128 and
    (.tmpfs | keys == ["/tmp", "/var/lib/postgresql", "/var/run/postgresql"]) and
    .tmpfs["/var/lib/postgresql"] == "rw,noexec,nosuid,nodev,size=536870912,mode=0700,uid=70,gid=70" and
    .tmpfs["/var/run/postgresql"] == "rw,noexec,nosuid,nodev,size=16777216,mode=0775" and
    .tmpfs["/tmp"] == "rw,noexec,nosuid,nodev,size=16777216,mode=1777" and
    all(.mounts[]; .Type == "tmpfs")
  ' <<<"$boundary" >/dev/null || fail 'RAM fixture mount boundary differs'
  printf 'Disposable PostgreSQL RAM boundary verified\n'
fi

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
    env -u GOFLAGS GOENV=off GOWORK=off go run -p "$go_parallelism" ./cmd/cli "$@"
}

if [[ "$test_pattern" == --review-qa-upgrade ]]; then
  (
    cd -- "$repository_root/services/internal/control-plane"
    KODEX_WORKFLOW_REVIEW_QA_UPGRADE_DSN="$dsn" \
      KODEX_WORKFLOW_REVIEW_QA_UPGRADE_DISPOSABLE=dedicated-loopback-container \
      env -u GOFLAGS GOENV=off GOWORK=off go test -p "$go_parallelism" -v -count=1 -timeout=180s \
      ./cmd/cli -run "$upgrade_test"
  )
  if [[ "$volume_owner" == true ]]; then
    # Полная native017+SSH уже проверена выше; release-verifiers прежних схем не расширяются.
    psql "$admin_dsn" --no-password -v ON_ERROR_STOP=1 \
      -c 'CREATE DATABASE control_plane_component_template WITH TEMPLATE control_plane OWNER control_plane_owner' >/dev/null
    (
      cd -- "$repository_root/services/internal/control-plane"
      KODEX_CONTROL_PLANE_TEST_DSN="$runtime_dsn" \
        KODEX_CONTROL_PLANE_TEST_ADMIN_DSN="$admin_dsn" \
        KODEX_CONTROL_PLANE_TEST_TEMPLATE_DATABASE=control_plane_component_template \
        env -u GOFLAGS GOENV=off GOWORK=off go test -p "$go_parallelism" -v -count=1 -timeout=120s \
        ./internal/repository/postgres/platform -run '^TestWorkspaceWriterOwnerComponent$'
    )
  fi
  exit
fi

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
      env -u GOFLAGS GOENV=off GOWORK=off go test -p "$go_parallelism" -count=1 -timeout=90s ./cmd/cli -run '^TestScheduleProtocolUpgrade$'
  fi
  run_migration up
  run_migration status >/dev/null
  run_migration up
  # Каждый target проверяется собственным закрытым verifier.010 не принимает011;
  # неизвестная схема не получает обхода либо fallback к старому профилю.
  schema_version=$(psql "postgresql://postgres@127.0.0.1:${port}/control_plane?sslmode=disable" --no-password -X -qAt -v ON_ERROR_STOP=1 \
    -c 'SELECT max(version_id) FILTER (WHERE is_applied) FROM public.goose_db_version')
  case "$schema_version" in
    20261028000100) schema_proof=workspace-restore-migration-postcondition.sql; schema_verifier=validWorkspaceRestoreMigrationPostcondition ;;
    20261027000100)
      # Одинаковая конечная Goose version не отличает010+SSH от полного017+SSH.
      # Выбираем отдельный closed verifier по фактически применённому017;
      # прежняя SSH-проверка не принимает новые таблицы либо урезанную проекцию.
      volume_migration_count=$(psql "postgresql://postgres@127.0.0.1:${port}/control_plane?sslmode=disable" --no-password -X -qAt -v ON_ERROR_STOP=1 \
        -c 'SELECT count(*) FROM public.goose_db_version WHERE version_id=20261026001700 AND is_applied')
      case "$volume_migration_count" in
        0) schema_proof=ssh-compose-migration-postcondition.sql; schema_verifier=validSSHComposeMigrationPostcondition ;;
        1) schema_proof=workspace-volume-ssh-migration-postcondition.sql; schema_verifier=validWorkspaceVolumeSSHMigrationPostcondition ;;
        *) fail 'workspace volume migration history is ambiguous' ;;
      esac ;;
    20261026001600) schema_proof=workspace-checkpoint-migration-postcondition.sql; schema_verifier=validWorkspaceCheckpointMigrationPostcondition ;;
    20261026001500) schema_proof=session-workspace-migration-postcondition.sql; schema_verifier=validSessionWorkspaceMigrationPostcondition ;;
    20261026001000) schema_proof=server-workflow-migration-postcondition.sql; schema_verifier=validServerWorkflowMigrationPostcondition ;;
    20261026001400) schema_proof=workflow-review-qa-evidence-migration-postcondition.sql; schema_verifier=validWorkflowReviewQAEvidenceMigrationPostcondition ;;
    20261026001100) schema_proof=legacy-workflow-rework-migration-postcondition.sql; schema_verifier=validLegacyWorkflowReworkMigrationPostcondition ;;
    20261026001200) schema_proof=workflow-exception-context-migration-postcondition.sql; schema_verifier=validWorkflowExceptionContextMigrationPostcondition ;;
    20261026001300) schema_proof=workflow-upstream-rework-migration-postcondition.sql; schema_verifier=validWorkflowUpstreamReworkMigrationPostcondition ;;
    *) fail 'unsupported current workflow schema target' ;;
  esac
  psql "postgresql://postgres@127.0.0.1:${port}/control_plane?sslmode=disable" \
    --no-password -X -qAt -v ON_ERROR_STOP=1 \
    --file "$repository_root/tools/release/$schema_proof" | \
    node --input-type=module -e '
      const {pathToFileURL} = await import("node:url");
      const verifiers = await import(pathToFileURL(process.argv[1]));
      let input = "";
      for await (const chunk of process.stdin) input += chunk;
      if (!Object.hasOwn(verifiers, process.argv[2]) || !verifiers[process.argv[2]](JSON.parse(input)))
        throw new Error("Current workflow schema postcondition failed");
      process.stdout.write("Current workflow schema read-only query passed\n");
    ' "$repository_root/tools/release/control-plane-immutable-migration.mjs" "$schema_verifier"
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
    env -u GOFLAGS GOENV=off GOWORK=off go test -p "$go_parallelism" -v -count=1 \
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
      KODEX_CONTROL_PLANE_TEST_ADMIN_DSN="$admin_dsn" \
      KODEX_CONTROL_PLANE_TEST_TEMPLATE_DATABASE=control_plane_component_template \
      env -u GOFLAGS GOENV=off GOWORK=off go test -p "$go_parallelism" -count=1 \
        ./internal/repository/postgres/platform -run '^TestAvatarLifecycleComponent$'
  fi
)

printf 'Control-plane PostgreSQL tests passed\n'
