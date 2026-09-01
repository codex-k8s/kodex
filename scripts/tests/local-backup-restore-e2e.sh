#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Local backup restore E2E failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    'Usage: local-backup-restore-e2e.sh --context <exact-context>' \
    '  --kubeconfig <path> --state-directory <path>' \
    '  [--backup-id <verified-backup-id>]' \
    '  [--expected-object-key <canonical-session-archive-key>' \
    '   --expected-object-file <private-file>]' >&2
}

context=""
kubeconfig=""
state_directory=""
requested_backup_id=""
expected_object_key=""
expected_object_file=""
while (($# > 0)); do
  case "$1" in
    --context) context=${2:-}; shift 2 ;;
    --kubeconfig) kubeconfig=${2:-}; shift 2 ;;
    --state-directory) state_directory=${2:-}; shift 2 ;;
    --backup-id) requested_backup_id=${2:-}; shift 2 ;;
    --expected-object-key) expected_object_key=${2:-}; shift 2 ;;
    --expected-object-file) expected_object_file=${2:-}; shift 2 ;;
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
if [[ -n "$requested_backup_id" && ! "$requested_backup_id" =~ ^[0-9]{8}T[0-9]{6}Z-[a-f0-9]{16}$ ]]; then
  fail 'requested verified backup identifier is invalid'
fi
if [[ -n "$expected_object_key" || -n "$expected_object_file" ]]; then
  [[ "$expected_object_key" =~ ^session-archive/v1/org_[a-z0-9]+/prj_[a-z0-9]+/ses_[a-z0-9]+/g[1-9][0-9]*/sat_[a-z0-9]+-a[1-9][0-9]*\.tar$ ]] ||
    fail 'expected session archive object key is not canonical'
  [[ "$expected_object_file" == /* && -f "$expected_object_file" && -s "$expected_object_file" &&
    ! -L "$expected_object_file" ]] || fail 'expected session archive object file is absent or unsafe'
fi
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
temporary_directory=$(mktemp -d "$state_directory/e2e/backup-restore.XXXXXX")
chmod 0700 "$temporary_directory"
suffix=$(date -u +%Y%m%d%H%M%S)-$$
restore_id="e2e-restore-$suffix"
approval_id="e2e-approval-$suffix"
target_prefix="$restore_id"
job_name="backup-controller-$restore_id"
job_selector="app.kubernetes.io/name=backup-controller,app.kubernetes.io/component=restore-drill,app.kubernetes.io/managed-by=kodex-local-e2e,kodex.dev/local-profile=hot-reload,kodex.dev/e2e-run=$suffix"
target_databases=()
port_forward_pid=""
cleanup() {
  if [[ -n "$port_forward_pid" ]]; then
    kill "$port_forward_pid" >/dev/null 2>&1 || true
    wait "$port_forward_pid" >/dev/null 2>&1 || true
  fi
  kodex_e2e_delete_owned_jobs kodex-system "$job_selector" \
    '^backup-controller-e2e-restore-[0-9]{14}-[0-9]+$' 2m >/dev/null 2>&1 || true
  kubectl -n kodex-system delete secret/backup-controller-repository \
    secret/backup-controller-restore-targets secret/backup-controller-restore-approval \
    --ignore-not-found --wait=false >/dev/null 2>&1 || true
  for database in "${target_databases[@]}"; do
    [[ "$database" =~ ^[a-z][a-z0-9_]{0,62}$ ]] || continue
    kubectl -n kodex-system exec statefulset/kodex-postgresql -c postgresql -- \
      psql --username=postgres --dbname=postgres --set=ON_ERROR_STOP=1 \
        --command "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '$database' AND pid <> pg_backend_pid();" \
        --command "DROP DATABASE IF EXISTS \"$database\";" >/dev/null 2>&1 || true
  done
  rm -rf -- "$temporary_directory"
}
trap cleanup EXIT

status=$(kubectl -n kodex-system exec deployment/backup-controller -c backup-controller -- \
  wget -qO- http://127.0.0.1:9090/status 2>/dev/null) || fail 'backup-controller status is unavailable'
if [[ -n "$requested_backup_id" ]]; then
  jq -e 'select(.state == "idle")' <<<"$status" >/dev/null ||
    fail 'backup-controller is not idle for the requested restore'
  backup_id=$requested_backup_id
else
  backup_id=$(jq -er 'select(.state == "idle") | .lastVerifiedBackup | select(type == "string" and length > 0)' <<<"$status") ||
    fail 'backup-controller has no verified backup ready for restore'
fi

credentials="$temporary_directory/credentials.json"
kubectl -n kodex-system get secret/backup-controller-credentials -o json | \
  jq -er '.data["credentials.json"] | @base64d' >"$credentials"
chmod 0600 "$credentials"
jq -e '
  .schemaVersion == 1 and (.databases | length) > 0 and
  .destination.bucket == "kodex-backups" and
  (.objectStores | length) > 0
' "$credentials" >/dev/null || fail 'local backup-controller credentials are invalid'

postgres_password=$(kubectl -n kodex-system get secret/kodex-postgresql-bootstrap -o json | \
  jq -er '.data.password | @base64d') || fail 'local PostgreSQL bootstrap credential is unavailable'
[[ -n "$postgres_password" && "$postgres_password" != *$'\n'* ]] || fail 'local PostgreSQL bootstrap credential is invalid'

targets="$temporary_directory/targets.json"
control_plane_database="e2e_cp_${suffix//-/}"
authority_database="e2e_ira_${suffix//-/}"
control_plane_database=${control_plane_database:0:63}
authority_database=${authority_database:0:63}
target_databases=("$control_plane_database" "$authority_database")
POSTGRES_PASSWORD="$postgres_password" jq -n --slurpfile credentials "$credentials" \
  --arg control_plane_database "$control_plane_database" \
  --arg authority_database "$authority_database" \
  --arg target_prefix "$target_prefix" '
  ($credentials[0]) as $source |
  {
    schemaVersion: 1,
    databases: ($source.databases | map({
      name,
      host,
      port,
      adminDatabase: "postgres",
      database: (if .name == "control-plane" then $control_plane_database
                 elif .name == "internal-rpc-authority" then $authority_database
                 else error("unexpected backup database") end),
      user: "postgres",
      password: env.POSTGRES_PASSWORD,
      tlsMode,
      tlsServerName,
      caFile
    })),
    objectStore: {
      name: "restore-fixture",
      endpoint: $source.destination.endpoint,
      region: $source.destination.region,
      bucket: "kodex-restore-fixture",
      prefix: $target_prefix,
      accessKeyId: $source.destination.accessKeyId,
      secretAccessKey: $source.destination.secretAccessKey,
      usePathStyle: true,
      allowInsecureLocal: true
    }
  }
' >"$targets"
chmod 0400 "$targets"

repository="$temporary_directory/repository.json"
jq '{schemaVersion: 1, destination: .destination}' "$credentials" >"$repository"
chmod 0400 "$repository"

fingerprint=$(
  cd "$repository_root/services/jobs/backup-controller"
  DEPLOYMENT_ENVIRONMENT=staging \
  BACKUP_CONTROLLER_RESTORE_TARGETS_FILE="$targets" \
    go run ./cmd/backup-controller fingerprint-targets
)
target_digest=$(jq -er '.targetSetSha256 | select(test("^sha256:[a-f0-9]{64}$"))' <<<"$fingerprint") ||
  fail 'restore target fingerprint is unavailable'

approval="$temporary_directory/approval.json"
jq -n \
  --arg approval_id "$approval_id" \
  --arg restore_id "$restore_id" \
  --arg backup_id "$backup_id" \
  --arg target_digest "$target_digest" \
  --arg expires_at "$(date -u -d '+2 hours' +%Y-%m-%dT%H:%M:%SZ)" '
  {
    schemaVersion: 1,
    approvalId: $approval_id,
    restoreId: $restore_id,
    backupId: $backup_id,
    targetSetSha256: $target_digest,
    expiresAt: $expires_at
  }
' >"$approval"
chmod 0400 "$approval"

apply_private_secret() {
  local name=$1 key=$2 file=$3
  kubectl -n kodex-system create secret generic "$name" --from-file="$key=$file" \
    --dry-run=client -o yaml | yq '
      .metadata.labels = {
        "app.kubernetes.io/part-of":"kodex",
        "app.kubernetes.io/name":"backup-controller",
        "app.kubernetes.io/component":"restore-drill",
        "app.kubernetes.io/managed-by":"tools-dev",
        "kodex.dev/local-profile":"hot-reload"
      }
    ' | kubectl apply --server-side --force-conflicts --field-manager=kodex-local-e2e -f - >/dev/null
}
apply_private_secret backup-controller-repository repository.json "$repository"
apply_private_secret backup-controller-restore-targets targets.json "$targets"
apply_private_secret backup-controller-restore-approval approval.json "$approval"

image=$(kubectl -n kodex-system get deployment/backup-controller -o json | jq -er '
  .spec.template.spec.containers[] | select(.name == "backup-controller") | .image |
  select(test("@sha256:[a-f0-9]{64}$"))
') || fail 'backup-controller exact image is unavailable'
digest=${image##*@sha256:}
job_manifest="$temporary_directory/restore-job.yaml"
RESTORE_JOB_NAME="$job_name" E2E_RUN="$suffix" \
BACKUP_CONTROLLER_IMAGE="$image" BACKUP_CONTROLLER_DIGEST="$digest" \
  yq '
    .metadata.name = strenv(RESTORE_JOB_NAME) |
    .metadata.labels["app.kubernetes.io/part-of"] = "kodex" |
    .metadata.labels["app.kubernetes.io/managed-by"] = "kodex-local-e2e" |
    .metadata.labels["kodex.dev/e2e-run"] = strenv(E2E_RUN) |
    .metadata.labels["kodex.dev/environment"] = "staging" |
    .metadata.labels["kodex.dev/local-profile"] = "hot-reload" |
    .spec.activeDeadlineSeconds = 900 |
    .spec.ttlSecondsAfterFinished = 600 |
    .spec.template.metadata.labels["app.kubernetes.io/part-of"] = "kodex" |
    .spec.template.metadata.labels["app.kubernetes.io/managed-by"] = "kodex-local-e2e" |
    .spec.template.metadata.labels["kodex.dev/e2e-run"] = strenv(E2E_RUN) |
    .spec.template.metadata.labels["kodex.dev/environment"] = "staging" |
    .spec.template.metadata.labels["kodex.dev/local-profile"] = "hot-reload" |
    (.spec.template.spec.containers[] | select(.name == "restore-drill") | .image) = strenv(BACKUP_CONTROLLER_IMAGE) |
    (.spec.template.spec.containers[] | select(.name == "restore-drill") |
      .env[] | select(.name == "BACKUP_CONTROLLER_RELEASE_REVISION") | .value) =
      ("sha256:" + strenv(BACKUP_CONTROLLER_DIGEST))
  ' "$repository_root/deploy/k8s/base/backup-controller/restore-drill-job.template.yaml" >"$job_manifest"
kubectl -n kodex-system apply --server-side --force-conflicts --field-manager=kodex-local-e2e \
  -f "$job_manifest" >/dev/null
if ! kodex_e2e_wait_job_complete kodex-system "$job_name" 900; then
  fail 'disposable restore drill did not complete'
fi

for database in "${target_databases[@]}"; do
  exists=$(kubectl -n kodex-system exec statefulset/kodex-postgresql -c postgresql -- \
    psql --username=postgres --dbname=postgres --no-align --tuples-only --set=ON_ERROR_STOP=1 \
      --command "SELECT count(*) FROM pg_database WHERE datname = '$database';" 2>/dev/null | sed '/^[[:space:]]*$/d')
  [[ "$exists" == 1 ]] || fail 'restored PostgreSQL database readback failed'
done

jq -er '.destination.accessKeyId' "$credentials" >"$temporary_directory/access-key"
jq -er '.destination.secretAccessKey' "$credentials" >"$temporary_directory/secret-key"
chmod 0600 "$temporary_directory/access-key" "$temporary_directory/secret-key"

port_forward_log="$temporary_directory/port-forward.log"
kodex_e2e_start_seaweedfs_port_forward kodex-system "$port_forward_log" ||
  fail 'ready SeaweedFS Service endpoint or loopback port-forward is unavailable'
port_forward_pid=$KODEX_E2E_PORT_FORWARD_PID
endpoint=$KODEX_E2E_PORT_FORWARD_ENDPOINT
(
  cd "$repository_root/services/jobs/backup-controller"
  BACKUP_RESTORE_E2E=1 \
  BACKUP_RESTORE_E2E_ENDPOINT="$endpoint" \
  BACKUP_RESTORE_E2E_ACCESS_KEY_FILE="$temporary_directory/access-key" \
  BACKUP_RESTORE_E2E_SECRET_KEY_FILE="$temporary_directory/secret-key" \
  BACKUP_RESTORE_E2E_BACKUP_ID="$backup_id" \
  BACKUP_RESTORE_E2E_RESTORE_ID="$restore_id" \
  BACKUP_RESTORE_E2E_TARGET_PREFIX="$target_prefix" \
  BACKUP_RESTORE_E2E_EXPECTED_OBJECT_KEY="$expected_object_key" \
  BACKUP_RESTORE_E2E_EXPECTED_OBJECT_FILE="$expected_object_file" \
    go test ./internal/s3backup -run '^TestSeaweedFSBackupRestoreDrillReadbackE2E$' \
      -count=1 -timeout=2m
)

printf 'Local verified backup and disposable restore drill passed.\n'
