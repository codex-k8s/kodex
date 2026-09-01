#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Local session archive backup restore E2E failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    'Usage: local-session-archive-backup-restore-e2e.sh --context <exact-context>' \
    '  --kubeconfig <path> --state-directory <path>' >&2
}

context=""
kubeconfig=""
state_directory=""
while (($# > 0)); do
  case "$1" in
    --context) context=${2:-}; shift 2 ;;
    --kubeconfig) kubeconfig=${2:-}; shift 2 ;;
    --state-directory) state_directory=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

confirmation=I_UNDERSTAND_THIS_MUTATES_A_DISPOSABLE_INSTALLATION
[[ ${KODEX_E2E_CONFIRM_DISPOSABLE:-} == "$confirmation" ]] ||
  fail 'explicit disposable installation confirmation is required'
[[ -n "$context" && "${context,,}" != *prod* && "${context,,}" != *production* ]] ||
  fail 'exact non-production context is required'
[[ "$kubeconfig" == /* && -f "$kubeconfig" && -r "$kubeconfig" && ! -L "$kubeconfig" ]] ||
  fail 'Kubernetes configuration is absent or unsafe'
[[ "$state_directory" == /* && -d "$state_directory" && ! -L "$state_directory" ]] ||
  fail 'state directory is absent or unsafe'
for command_name in date go head jq kubectl sed sleep yq; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done

export KUBECONFIG="$kubeconfig"
[[ $(kubectl config current-context) == "$context" ]] || fail 'Kubernetes context mismatch'
kubectl get --raw=/readyz >/dev/null || fail 'Kubernetes API is unavailable'
kubectl get namespace/kodex-system -o json | jq -e '
  .metadata.labels["app.kubernetes.io/part-of"] == "kodex" and
  .metadata.labels["kodex.dev/environment"] == "staging" and
  .metadata.labels["kodex.dev/local-profile"] == "hot-reload"
' >/dev/null || fail 'Kodex namespace is not the exact disposable local profile'

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
source "$repository_root/scripts/tests/lib/local-kubernetes-e2e.sh"
kodex_e2e_ensure_private_directory "$state_directory/e2e" ||
  fail 'private E2E state directory is unavailable'
temporary_directory=$(mktemp -d "$state_directory/e2e/session-archive-backup.XXXXXX")
chmod 0700 "$temporary_directory"
suffix=$(date -u +%Y%m%d%H%M%S)-$$
fixture_id=${suffix//-/}
object_key="session-archive/v1/org_e2e00001/prj_e2e00001/ses_e2e00001/g1/sat_e2e${fixture_id}-a1.tar"
job_name="backup-session-e2e-${fixture_id: -20}"
stale_job_selector='app.kubernetes.io/name=backup-controller,app.kubernetes.io/component=backup-e2e,app.kubernetes.io/managed-by=kodex-local-e2e,kodex.dev/local-profile=hot-reload'
expected_file="$temporary_directory/session-archive.tar"
mutated_file="$temporary_directory/session-archive-mutated.tar"
backup_id_file="$temporary_directory/backup-id"
port_forward_pid=""
fixture_prepared=0

cleanup() {
  kodex_e2e_delete_owned_jobs kodex-system "$stale_job_selector" \
    '^backup-session-e2e-[0-9]{15,20}$' 2m >/dev/null 2>&1 || true
  if [[ "$fixture_prepared" == 1 && -n "${endpoint:-}" ]]; then
    (
      cd "$repository_root/services/jobs/backup-controller"
      BACKUP_SESSION_ARCHIVE_E2E=1 \
      BACKUP_SESSION_ARCHIVE_E2E_PHASE=cleanup \
      BACKUP_SESSION_ARCHIVE_E2E_ENDPOINT="$endpoint" \
      BACKUP_SESSION_ARCHIVE_E2E_ACCESS_KEY_FILE="$temporary_directory/access-key" \
      BACKUP_SESSION_ARCHIVE_E2E_SECRET_KEY_FILE="$temporary_directory/secret-key" \
      BACKUP_SESSION_ARCHIVE_E2E_OBJECT_KEY="$object_key" \
        go test ./internal/s3backup -run '^TestSeaweedFSSessionArchiveBackupFixtureE2E$' \
          -count=1 -timeout=2m >/dev/null 2>&1
    ) || true
  fi
  if [[ -n "$port_forward_pid" ]]; then
    kill "$port_forward_pid" >/dev/null 2>&1 || true
    wait "$port_forward_pid" >/dev/null 2>&1 || true
  fi
  rm -rf -- "$temporary_directory"
}
trap cleanup EXIT
kodex_e2e_delete_owned_jobs kodex-system "$stale_job_selector" \
  '^backup-session-e2e-[0-9]{15,20}$' 2m || fail 'stale backup E2E Job cleanup failed'

printf 'kodex canonical session archive backup fixture %s\n' "$suffix" >"$expected_file"
printf 'kodex mutated session archive fixture %s\n' "$suffix" >"$mutated_file"
chmod 0600 "$expected_file" "$mutated_file"

kubectl -n kodex-system get secret/backup-controller-credentials -o json | jq -er '
  .data["credentials.json"] | @base64d | fromjson |
  select(.schemaVersion == 1) |
  [.objectStores[] | select(.name == "session-archives")] |
  select(length == 1) | .[0].accessKeyId
' >"$temporary_directory/access-key" || fail 'local backup access key is unavailable'
kubectl -n kodex-system get secret/backup-controller-credentials -o json | jq -er '
  .data["credentials.json"] | @base64d | fromjson |
  select(.schemaVersion == 1) |
  [.objectStores[] | select(.name == "session-archives")] |
  select(length == 1) | .[0].secretAccessKey
' >"$temporary_directory/secret-key" || fail 'local backup secret key is unavailable'
chmod 0600 "$temporary_directory/access-key" "$temporary_directory/secret-key"

port_forward_log="$temporary_directory/port-forward.log"
kodex_e2e_start_seaweedfs_port_forward kodex-system "$port_forward_log" ||
  fail 'ready SeaweedFS Service endpoint or loopback port-forward is unavailable'
port_forward_pid=$KODEX_E2E_PORT_FORWARD_PID
endpoint=$KODEX_E2E_PORT_FORWARD_ENDPOINT

run_fixture_oracle() {
  local phase=$1 fixture_file=$2
  (
    cd "$repository_root/services/jobs/backup-controller"
    BACKUP_SESSION_ARCHIVE_E2E=1 \
    BACKUP_SESSION_ARCHIVE_E2E_PHASE="$phase" \
    BACKUP_SESSION_ARCHIVE_E2E_ENDPOINT="$endpoint" \
    BACKUP_SESSION_ARCHIVE_E2E_ACCESS_KEY_FILE="$temporary_directory/access-key" \
    BACKUP_SESSION_ARCHIVE_E2E_SECRET_KEY_FILE="$temporary_directory/secret-key" \
    BACKUP_SESSION_ARCHIVE_E2E_OBJECT_KEY="$object_key" \
    BACKUP_SESSION_ARCHIVE_E2E_FIXTURE_FILE="$fixture_file" \
    BACKUP_SESSION_ARCHIVE_E2E_STARTED_AT="${backup_started_at:-}" \
    BACKUP_SESSION_ARCHIVE_E2E_BACKUP_ID_FILE="$backup_id_file" \
      go test ./internal/s3backup -run '^TestSeaweedFSSessionArchiveBackupFixtureE2E$' \
        -count=1 -timeout=2m
  )
}

run_fixture_oracle prepare "$expected_file"
fixture_prepared=1
backup_started_at=$(date -u +%Y-%m-%dT%H:%M:%S.%NZ)

deployment="$temporary_directory/backup-controller-deployment.json"
job_manifest="$temporary_directory/backup-job.json"
kubectl -n kodex-system get deployment/backup-controller -o json >"$deployment" ||
  fail 'backup-controller deployment is unavailable'
jq --arg job_name "$job_name" '
  {
    apiVersion: "batch/v1",
    kind: "Job",
    metadata: {
      name: $job_name,
      labels: {
        "app.kubernetes.io/name": "backup-controller",
        "app.kubernetes.io/component": "backup-e2e",
        "app.kubernetes.io/part-of": "kodex",
        "app.kubernetes.io/managed-by": "kodex-local-e2e",
        "kodex.dev/environment": "staging",
        "kodex.dev/local-profile": "hot-reload"
      }
    },
    spec: {
      backoffLimit: 0,
      activeDeadlineSeconds: 900,
      ttlSecondsAfterFinished: 600,
      template: .spec.template
    }
  } |
  .spec.template.metadata.labels["app.kubernetes.io/component"] = "backup-e2e" |
  .spec.template.metadata.labels["app.kubernetes.io/managed-by"] = "kodex-local-e2e" |
  .spec.template.metadata.labels["kodex.dev/environment"] = "staging" |
  .spec.template.metadata.labels["kodex.dev/local-profile"] = "hot-reload" |
  .spec.template.spec.restartPolicy = "Never" |
  .spec.template.spec.containers |= map(
    if .name == "backup-controller" then
      .args = ["backup"] |
      del(.ports, .startupProbe, .readinessProbe, .livenessProbe)
    else . end
  )
' "$deployment" >"$job_manifest" || fail 'build one-shot backup Job manifest'
kubectl -n kodex-system apply --server-side --force-conflicts --field-manager=kodex-local-e2e \
  -f "$job_manifest" >/dev/null
if ! kodex_e2e_wait_job_complete kodex-system "$job_name" 900; then
  fail 'one-shot backup did not complete'
fi

run_fixture_oracle find-backup "$expected_file"
backup_id=$(tr -d '\r\n' <"$backup_id_file")
[[ "$backup_id" =~ ^[0-9]{8}T[0-9]{6}Z-[a-f0-9]{16}$ ]] ||
  fail 'one-shot verified backup identifier readback failed'
run_fixture_oracle mutate "$mutated_file"

KODEX_E2E_CONFIRM_DISPOSABLE="$confirmation" \
  "$repository_root/scripts/tests/local-backup-restore-e2e.sh" \
    --context "$context" \
    --kubeconfig "$kubeconfig" \
    --state-directory "$state_directory" \
    --backup-id "$backup_id" \
    --expected-object-key "$object_key" \
    --expected-object-file "$expected_file"

run_fixture_oracle cleanup "$expected_file"
fixture_prepared=0
printf 'Local nonzero session archive backup and exact-byte restore passed.\n'
