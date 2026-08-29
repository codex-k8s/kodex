#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Local artifact storage E2E failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    'Usage: local-artifact-storage-e2e.sh capture|assert-absent|accelerate-retention' \
    '  --context <exact-context> --kubeconfig <path> --state-directory <path>' \
    '  --artifact-ref <ref> --receipt <private-json-path>' >&2
}

mode=${1:-}
[[ -n "$mode" ]] || { usage; exit 1; }
shift
context=""
kubeconfig=""
state_directory=""
artifact_ref=""
receipt=""
while (($# > 0)); do
  case "$1" in
    --context) context=${2:-}; shift 2 ;;
    --kubeconfig) kubeconfig=${2:-}; shift 2 ;;
    --state-directory) state_directory=${2:-}; shift 2 ;;
    --artifact-ref) artifact_ref=${2:-}; shift 2 ;;
    --receipt) receipt=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done
case "$mode" in capture|assert-absent|accelerate-retention) ;; *) usage; fail 'mode is invalid' ;; esac

confirmation=I_UNDERSTAND_THIS_MUTATES_A_DISPOSABLE_INSTALLATION
[[ ${KODEX_E2E_CONFIRM_DISPOSABLE:-} == "$confirmation" ]] ||
  fail 'explicit disposable installation confirmation is required'
[[ -n "$context" && "${context,,}" != *prod* && "${context,,}" != *production* ]] ||
  fail 'exact non-production context is required'
[[ "$kubeconfig" == /* && -f "$kubeconfig" && -r "$kubeconfig" && ! -L "$kubeconfig" ]] ||
  fail 'Kubernetes configuration is absent or unsafe'
[[ "$state_directory" == /* && -d "$state_directory" && ! -L "$state_directory" ]] ||
  fail 'state directory is absent or unsafe'
[[ "$receipt" == "$state_directory"/e2e/* && "$receipt" == *.json && ! -L "$receipt" ]] ||
  fail 'receipt must be a private JSON file under the E2E state directory'
[[ "$artifact_ref" =~ ^[A-Za-z0-9_-]{8,96}$ ]] || fail 'artifact reference is invalid'
for command_name in go head jq kubectl sed seq sleep stat wc; do
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
temporary_directory=$(mktemp -d "$state_directory/e2e/artifact-storage.XXXXXX")
port_forward_pid=""
s3_endpoint=""
cleanup() {
  if [[ -n "$port_forward_pid" ]]; then
    kill "$port_forward_pid" >/dev/null 2>&1 || true
    wait "$port_forward_pid" >/dev/null 2>&1 || true
  fi
  rm -rf -- "$temporary_directory"
}
trap cleanup EXIT

query_database() {
  local sql=$1
  kubectl -n kodex-system exec statefulset/kodex-postgresql -c postgresql -- \
    psql --username=postgres --dbname=control_plane --no-align --tuples-only \
      --set=ON_ERROR_STOP=1 --command "$sql" 2>/dev/null | sed '/^[[:space:]]*$/d'
}

capture_locator() {
  local state temporary
  state=$(query_database "
    SELECT json_build_object(
      'version', 1,
      'artifactRef', artifact.ref,
      'lifecycleState', artifact.lifecycle_state,
      'objectKey', content.object_key,
      'objectVersion', content.object_version
    )::text
    FROM control_plane.artifacts AS artifact
    JOIN control_plane.artifact_content AS content ON content.artifact_id = artifact.id
    WHERE artifact.ref = '$artifact_ref';")
  [[ $(printf '%s\n' "$state" | wc -l) -eq 1 ]] || fail 'artifact exact object locator is unavailable'
  jq -e --arg artifact_ref "$artifact_ref" '
    .version == 1 and .artifactRef == $artifact_ref and
    (.lifecycleState == "ACTIVE" or .lifecycleState == "DELETED") and
    (.objectKey | type == "string" and length > 0) and
    (.objectVersion | type == "string" and length > 0)
  ' <<<"$state" >/dev/null || fail 'artifact exact object locator is invalid'
  temporary=$(mktemp "$receipt.XXXXXX")
  printf '%s\n' "$state" >"$temporary"
  chmod 0600 "$temporary"
  mv -- "$temporary" "$receipt"
}

read_receipt() {
  [[ -f "$receipt" && ! -L "$receipt" && $(stat -c '%a' "$receipt") == 600 ]] ||
    fail 'private artifact receipt is absent or unsafe'
  jq -e --arg artifact_ref "$artifact_ref" '
    .version == 1 and .artifactRef == $artifact_ref and
    (.objectKey | type == "string" and length > 0) and
    (.objectVersion | type == "string" and length > 0)
  ' "$receipt" >/dev/null || fail 'private artifact receipt does not match the requested artifact'
}

assert_database_purged() {
  local state
  state=$(query_database "
    SELECT artifact.lifecycle_state || '|' ||
           CASE WHEN content.artifact_id IS NULL THEN 'absent' ELSE 'present' END
    FROM control_plane.artifacts AS artifact
    LEFT JOIN control_plane.artifact_content AS content ON content.artifact_id = artifact.id
    WHERE artifact.ref = '$artifact_ref';")
  [[ "$state" == 'PURGED|absent' ]] || fail 'artifact authoritative tombstone is not finalized'
}

prepare_object_storage() {
  local secret_state count port_forward_log endpoint
  secret_state="$temporary_directory/object-storage.json"
  kubectl -n kodex-system get secrets -l kodex.dev/local-credential=object-storage -o json >"$secret_state"
  chmod 0600 "$secret_state"
  count=$(jq '.items | length' "$secret_state")
  [[ "$count" == 1 ]] || fail 'exactly one local object storage credential is required'
  jq -er '.items[0].data["access-key"] | @base64d' "$secret_state" >"$temporary_directory/access-key"
  jq -er '.items[0].data["secret-key"] | @base64d' "$secret_state" >"$temporary_directory/secret-key"
  chmod 0600 "$temporary_directory/access-key" "$temporary_directory/secret-key"

  port_forward_log="$temporary_directory/port-forward.log"
  kubectl -n kodex-system port-forward --address=127.0.0.1 service/seaweedfs-s3 :8333 \
    >"$port_forward_log" 2>&1 &
  port_forward_pid=$!
  for _ in $(seq 1 100); do
    endpoint=$(sed -nE 's/^Forwarding from 127\.0\.0\.1:([0-9]+) -> 8333$/http:\/\/127.0.0.1:\1/p' "$port_forward_log" | head -n 1)
    [[ -n "$endpoint" ]] && break
    kill -0 "$port_forward_pid" >/dev/null 2>&1 || fail 'SeaweedFS port-forward failed'
    sleep 0.1
  done
  [[ -n "$endpoint" ]] || fail 'SeaweedFS loopback endpoint was not established'
  s3_endpoint=$endpoint
}

assert_exact_version_absent() {
  local object_key object_version
  read_receipt
  assert_database_purged
  prepare_object_storage
  object_key=$(jq -r '.objectKey' "$receipt")
  object_version=$(jq -r '.objectVersion' "$receipt")
  (
    cd "$repository_root/services/jobs/artifact-retention"
    ARTIFACT_STORAGE_E2E=1 \
    ARTIFACT_STORAGE_E2E_ENDPOINT="$s3_endpoint" \
    ARTIFACT_STORAGE_E2E_ACCESS_KEY_FILE="$temporary_directory/access-key" \
    ARTIFACT_STORAGE_E2E_SECRET_KEY_FILE="$temporary_directory/secret-key" \
    ARTIFACT_STORAGE_E2E_OBJECT_KEY="$object_key" \
    ARTIFACT_STORAGE_E2E_OBJECT_VERSION="$object_version" \
      go test ./internal/retention -run '^TestSeaweedFSExactVersionAbsentE2E$' \
        -count=1 -timeout=1m
  )
}

accelerate_retention() {
  local updated state deadline
  read_receipt
  updated=$(query_database "
    UPDATE control_plane.artifacts
       SET purge_after = clock_timestamp() - interval '1 second'
     WHERE ref = '$artifact_ref' AND lifecycle_state = 'DELETED'
     RETURNING ref;")
  [[ "$updated" == "$artifact_ref" ]] || fail 'retention test clock fixture was not applied to exact tombstone'
  deadline=$((SECONDS + 180))
  while ((SECONDS < deadline)); do
    state=$(query_database "
      SELECT artifact.lifecycle_state || '|' ||
             CASE WHEN content.artifact_id IS NULL THEN 'absent' ELSE 'present' END
      FROM control_plane.artifacts AS artifact
      LEFT JOIN control_plane.artifact_content AS content ON content.artifact_id = artifact.id
      WHERE artifact.ref = '$artifact_ref';")
    [[ "$state" == 'PURGED|absent' ]] && break
    sleep 2
  done
  [[ "$state" == 'PURGED|absent' ]] || fail 'artifact-retention did not finalize the accelerated local tombstone'
  assert_exact_version_absent
}

case "$mode" in
  capture) capture_locator ;;
  assert-absent) assert_exact_version_absent ;;
  accelerate-retention) accelerate_retention ;;
esac
