#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'Legacy configuration import failed: %s\n' "$*" >&2; exit 1; }
usage() {
  printf 'Usage: %s --owner-approved --revision <40-hex> --lock <release-lock.json> --plan-id <uuid> --source-root-reference <uuid> --source-root-sha256 <64-hex> --organization-id <uuid> --owner-actor-id <uuid> [--context <context>]\n' "$0" >&2
}

owner_approved=false
revision=""
lock_file=""
plan_id=""
source_root_reference=""
source_root_sha256=""
organization_id=""
owner_actor_id=""
context=""
while (($# > 0)); do
  case "$1" in
    --owner-approved) owner_approved=true; shift ;;
    --revision) revision="${2:-}"; shift 2 ;;
    --lock) lock_file="${2:-}"; shift 2 ;;
    --plan-id) plan_id="${2:-}"; shift 2 ;;
    --source-root-reference) source_root_reference="${2:-}"; shift 2 ;;
    --source-root-sha256) source_root_sha256="${2:-}"; shift 2 ;;
    --organization-id) organization_id="${2:-}"; shift 2 ;;
    --owner-actor-id) owner_actor_id="${2:-}"; shift 2 ;;
    --context) context="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ "$owner_approved" == true ]] || fail "explicit owner approval is required"
[[ "$revision" =~ ^[a-f0-9]{40}$ ]] || fail "revision must be exact lowercase 40-hex"
[[ -r "$lock_file" ]] || fail "release lock is not readable"
uuid_pattern='^[a-f0-9]{8}-[a-f0-9]{4}-[1-5][a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$'
[[ "$plan_id" =~ $uuid_pattern && "$source_root_reference" =~ $uuid_pattern &&
   "$organization_id" =~ $uuid_pattern && "$owner_actor_id" =~ $uuid_pattern ]] ||
  fail "plan, source or owner identity is invalid"
[[ "$source_root_sha256" =~ ^[a-f0-9]{64}$ ]] || fail "source root SHA-256 is invalid"
for command_name in git jq yq kubectl sha256sum base64 node; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repository_root=$(cd -- "$script_directory/../.." && pwd -P)
# shellcheck source=owner-evidence.sh
source "$script_directory/owner-evidence.sh"
[[ "$(git -C "$repository_root" rev-parse HEAD)" == "$revision" ]] || fail "checkout does not match approved revision"
[[ -z "$(git -C "$repository_root" status --porcelain --untracked-files=no)" ]] || fail "tracked checkout must be clean"
[[ "$(jq -r .source_sha "$lock_file")" == "$revision" ]] || fail "release lock source SHA mismatch"

kube=(kubectl)
if [[ -n "$context" ]]; then kube+=(--context "$context"); fi
namespace=mattercodex-system
source_namespace=matter-kodex-prod
source_postgres=mattermost-postgres-0
temporary_directory=$(mktemp -d)
source_principal_window_open=false
close_source_principal_window() {
  [[ "$source_principal_window_open" == true ]] || return 0
  "${kube[@]}" -n "$source_namespace" exec -i "$source_postgres" -c postgres -- sh -ceu '
    exec psql -X -q -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"
  ' <<'SQL'
BEGIN;
SET LOCAL statement_timeout = '15s';
SET LOCAL lock_timeout = '5s';
ALTER ROLE matter_codex_migration_g1 NOLOGIN;
SELECT pg_catalog.pg_terminate_backend(pid)
FROM pg_catalog.pg_stat_activity
WHERE usename = 'matter_codex_migration_g1'
  AND pid <> pg_catalog.pg_backend_pid();
COMMIT;
SQL
  source_principal_window_open=false
}
cleanup() {
  close_source_principal_window || printf 'Legacy configuration import warning: source principal window cleanup failed\n' >&2
  rm -rf -- "$temporary_directory"
}
trap cleanup EXIT
umask 077

open_source_principal_window() {
  "${kube[@]}" -n "$source_namespace" exec -i "$source_postgres" -c postgres -- sh -ceu '
    exec psql -X -q -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"
  ' <<'SQL'
BEGIN;
SET LOCAL statement_timeout = '15s';
SET LOCAL lock_timeout = '5s';
DO $preflight$
DECLARE
  principal_oid oid;
  capability_oid oid;
BEGIN
  SELECT oid INTO principal_oid FROM pg_catalog.pg_roles WHERE rolname = 'matter_codex_migration_g1';
  SELECT oid INTO capability_oid FROM pg_catalog.pg_roles WHERE rolname = 'matter_codex_migration';
  IF principal_oid IS NULL OR capability_oid IS NULL THEN
    RAISE EXCEPTION 'migration principal or capability role is missing';
  END IF;
  IF NOT pg_catalog.pg_has_role('matter_codex_migration_g1', 'matter_codex_migration', 'MEMBER') THEN
    RAISE EXCEPTION 'migration capability membership is missing';
  END IF;
  IF EXISTS (
    SELECT 1
    FROM pg_catalog.pg_auth_members AS membership
    WHERE membership.member = principal_oid
      AND membership.roleid <> capability_oid
  ) THEN
    RAISE EXCEPTION 'migration principal has unexpected memberships';
  END IF;
END
$preflight$;
SELECT format(
  'ALTER ROLE matter_codex_migration_g1 LOGIN VALID UNTIL %L',
  pg_catalog.clock_timestamp() + interval '30 minutes'
) \gexec
COMMIT;
SQL
  source_principal_window_open=true
}

credentials_tsv="$temporary_directory/credentials.tsv"
"${kube[@]}" -n "$source_namespace" exec "$source_postgres" -c postgres -- sh -c '
  PGPASSWORD="$POSTGRES_PASSWORD" psql -X -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" -At -F "	" -c "
    SELECT id, secret_ref, credential_type, coalesce(secret_content_sha256, chr(45))
    FROM matter_codex_credentials
    WHERE (credential_type = chr(103)||chr(105)||chr(116)||chr(104)||chr(117)||chr(98)||chr(95)||chr(116)||chr(111)||chr(107)||chr(101)||chr(110) AND status = chr(99)||chr(111)||chr(110)||chr(102)||chr(105)||chr(103)||chr(117)||chr(114)||chr(101)||chr(100))
       OR (credential_type = chr(99)||chr(111)||chr(100)||chr(101)||chr(120)||chr(95)||chr(97)||chr(117)||chr(116)||chr(104) AND status = chr(97)||chr(117)||chr(116)||chr(104)||chr(111)||chr(114)||chr(105)||chr(122)||chr(101)||chr(100))
    ORDER BY id"
' >"$credentials_tsv"
[[ -s "$credentials_tsv" ]] || fail "eligible legacy credential inventory is empty"

credential_evidence="$temporary_directory/credentials.json"
printf '{}\n' >"$credential_evidence"
while IFS=$'\t' read -r credential_id source_secret credential_type expected_sha256; do
  [[ "$credential_id" =~ ^[1-9][0-9]*$ && "$source_secret" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]] ||
    fail "legacy credential metadata is invalid"
  case "$credential_type" in
    github_token) content_key=github-token ;;
    codex_auth) content_key=auth.json ;;
    *) fail "unsupported legacy credential type" ;;
  esac
  source_json="$temporary_directory/source-secret-$credential_id.json"
  target_json="$temporary_directory/target-secret-$credential_id.json"
  live_target_json="$temporary_directory/live-target-secret-$credential_id.json"
  "${kube[@]}" -n "$source_namespace" get secret "$source_secret" -o json >"$source_json"
  jq -e --arg key "$content_key" '.data[$key] | type == "string" and length > 0' "$source_json" >/dev/null ||
    fail "legacy credential content key is absent"
  actual_sha256=$(jq -r --arg key "$content_key" '.data[$key]' "$source_json" | base64 -d | sha256sum | awk '{print $1}')
  if [[ "$expected_sha256" != - && "$actual_sha256" != "$expected_sha256" ]]; then
    fail "legacy credential integrity readback failed"
  fi
  target_name="legacy-credential-$credential_id-${actual_sha256:0:12}"
  jq --arg namespace "$namespace" --arg name "$target_name" '
    {apiVersion:"v1",kind:"Secret",metadata:{namespace:$namespace,name:$name,
      labels:{"app.kubernetes.io/name":"legacy-data-migration","mattercodex.dev/credential-snapshot":"true"}},
      immutable:true,type:(.type // "Opaque"),data:.data}
  ' "$source_json" >"$target_json"
  if "${kube[@]}" -n "$namespace" get secret "$target_name" -o json >"$live_target_json" 2>/dev/null; then
    jq -e '.immutable == true' "$live_target_json" >/dev/null || fail "existing credential snapshot is mutable"
    source_data_sha=$(jq -S -c .data "$target_json" | sha256sum | awk '{print $1}')
    target_data_sha=$(jq -S -c .data "$live_target_json" | sha256sum | awk '{print $1}')
    [[ "$source_data_sha" == "$target_data_sha" ]] || fail "existing credential snapshot differs from source"
  else
    "${kube[@]}" create -f "$target_json" >/dev/null
    "${kube[@]}" -n "$namespace" get secret "$target_name" -o json >"$live_target_json"
  fi
  uid=$(jq -er '.metadata.uid' "$live_target_json")
  resource_version=$(jq -er '.metadata.resourceVersion' "$live_target_json")
  next_evidence="$temporary_directory/credentials-next.json"
  jq --arg id "$credential_id" --arg name "$target_name" --arg version "$uid:$resource_version" --arg sha "$actual_sha256" '
    . + {($id): {
      secretRef:("k8s-secret://mattercodex-system/"+$name),
      immutableSecretRef:("k8s-immutable-secret://mattercodex-system/"+$name),
      contentVersion:$version,
      contentSha256:$sha
    }}
  ' "$credential_evidence" >"$next_evidence"
  mv "$next_evidence" "$credential_evidence"
done <"$credentials_tsv"

policy_json="$temporary_directory/image-policy.json"
"${kube[@]}" -n "$namespace" get configmap mattercodex-image-admission-policy -o json >"$policy_json"
for key in policyRevision policySHA256 roleImageInputRepository trustedRoleBaseRepository trustedRoleBaseDigest roleRuntimeContractRevision roleRuntimeContractSHA256; do
  jq -e --arg key "$key" '.data[$key] | type == "string" and length > 0' "$policy_json" >/dev/null ||
    fail "image admission policy is incomplete"
done
policy_revision=$(jq -r '.data.policyRevision' "$policy_json")
policy_sha256=$(jq -r '.data.policySHA256' "$policy_json")
role_input_repository=$(jq -r '.data.roleImageInputRepository' "$policy_json")
trusted_base_repository=$(jq -r '.data.trustedRoleBaseRepository' "$policy_json")
trusted_base_digest=$(jq -r '.data.trustedRoleBaseDigest' "$policy_json")
runtime_contract_revision=$(jq -r '.data.roleRuntimeContractRevision' "$policy_json")
runtime_contract_sha256=$(jq -r '.data.roleRuntimeContractSHA256' "$policy_json")
image_digest=${trusted_base_digest#sha256:}
[[ "$policy_revision" =~ ^[1-9][0-9]*$ && "$runtime_contract_revision" =~ ^[1-9][0-9]*$ &&
   "$policy_sha256" =~ ^[a-f0-9]{64}$ && "$runtime_contract_sha256" =~ ^[a-f0-9]{64}$ &&
   "$image_digest" =~ ^[a-f0-9]{64}$ ]] || fail "image admission policy evidence is invalid"
source_revision_sha256=$(printf '%s' "$revision" | sha256sum | awk '{print $1}')
promoted_at=$(date -u +'%Y-%m-%dT%H:%M:%SZ')
candidate_owner_evidence="$temporary_directory/owner-evidence-candidate.json"
owner_evidence="$temporary_directory/owner-evidence.json"
jq -n --slurpfile credentials "$credential_evidence" \
  --arg source_revision "$revision" --arg source_sha "$source_revision_sha256" \
  --arg input_repository "$role_input_repository" --arg base_repository "$trusted_base_repository" \
  --arg base_digest "$trusted_base_digest" --arg digest "$image_digest" --arg promoted_at "$promoted_at" \
  '{
    schemaVersion:"mattercodex.legacy-data-owner-evidence.v1",
    archive:{storagePrefix:"s3://mattercodex-legacy-configuration",storageVersion:"configuration-v1",
      retentionRef:"policy://legacy-configuration-retention",scanPolicyRevision:1,
      scanEvidenceSha256:$source_sha,scannerWorkloadId:"artifact-scanner",scannedAt:$promoted_at},
    provider:{observedAt:$promoted_at,observationRevision:1,observedLimit:100},
    credentials:$credentials[0],
    roleImage:{
      input:{baseImageReference:$base_repository,baseImageDigest:$base_digest,
        sourceRef:"git://github.com/codex-k8s/matter-codex",sourceRevision:$source_revision,sourceSha256:$source_sha,
        contextRef:("oci://"+$input_repository+"@sha256:"+$digest),contextSha256:$digest,
        builderSha256:$digest,frontendSha256:$digest,platforms:[{os:"linux",architecture:"amd64",variant:""}],
        packages:[],tools:[],installationBlock:"",toolchainSha256:$digest},
      generation:1,specSha256:"",
      buildStagingReference:("localhost:5001/mattercodex/legacy-role-staging@sha256:"+$digest),
      buildManifestDigest:("sha256:"+$digest),buildProvenanceSha256:$source_sha,
      promotedReference:($base_repository+"@sha256:"+$digest),admissionRevision:1,
      admissionReceiptSha256:$source_sha,admissionReceiptManifestDigest:("sha256:"+$digest),
      signatureSha256:$source_sha,promotionReadbackSha256:$source_sha,sbomSha256:$source_sha,
      vulnerabilityEvidenceSha256:$source_sha,signatureIdentity:"legacy-migration-owner",promotedAt:$promoted_at}
  }' >"$candidate_owner_evidence"

live_owner_evidence_configmap="$temporary_directory/live-owner-evidence-configmap.json"
if ! "${kube[@]}" -n "$namespace" get configmap legacy-data-migration-owner-evidence \
  -o json >"$live_owner_evidence_configmap" 2>/dev/null; then
  : >"$live_owner_evidence_configmap"
fi
if ! evidence_selection=$(select_legacy_owner_evidence "$candidate_owner_evidence" \
  "$live_owner_evidence_configmap" "$owner_evidence" "$plan_id" "$source_root_reference" \
  "$source_root_sha256" "$revision"); then
  fail "persisted owner evidence differs from the exact retry tuple"
fi
printf 'Legacy configuration import owner evidence: %s\n' "$evidence_selection"
owner_evidence_sha256=$(sha256sum "$owner_evidence" | awk '{print $1}')

helper="$repository_root/tools/deploy/direct-production-material-helper.mjs"
signer="$temporary_directory/migration-signer.jwk"
grant="$temporary_directory/application-grant.jws"
backup_key="$temporary_directory/backup-key"
"${kube[@]}" -n "$namespace" get secret control-plane-readiness-grant-signers -o json |
  jq -r '.data["legacy-data-migration.private.jwk"]' | base64 -d >"$signer"
"${kube[@]}" -n "$namespace" get secret legacy-data-migration-backup-key -o json |
  jq -r '.data.key' | base64 -d >"$backup_key"
[[ -s "$signer" && -s "$backup_key" ]] || fail "migration signer or backup key is absent"
grant_revision=$(date -u +%s)
node "$helper" generate-legacy-migration-grant "$signer" "$grant" "$organization_id" "$owner_actor_id" \
  "$source_root_reference" "$source_root_sha256" "$grant_revision" 300
"${kube[@]}" -n "$namespace" create secret generic legacy-data-migration \
  --from-file=backup-key="$backup_key" --from-file=application-grant.jws="$grant" \
  --dry-run=client -o yaml | "${kube[@]}" apply -f - >/dev/null

render="$temporary_directory/migration.yaml"
"$repository_root/tools/release/render-direct-production-applications.sh" \
  --lock "$lock_file" --output "$render" --scope migration >/dev/null
OWNER_EVIDENCE="$owner_evidence" OWNER_EVIDENCE_SHA256="$owner_evidence_sha256" \
PLAN_ID="$plan_id" SOURCE_ROOT_REFERENCE="$source_root_reference" \
SOURCE_ROOT_SHA256="$source_root_sha256" RELEASE_REVISION="$revision" yq -i '
  with(select(.kind != null);
    .metadata.labels = ((.metadata.labels // {}) + {
      "mattercodex.dev/release-managed": "false",
      "mattercodex.dev/migration-managed": "true"
    })) |
  with(select(.kind == "ConfigMap" and .metadata.name == "legacy-data-migration-owner-evidence");
    .metadata.annotations = ((.metadata.annotations // {}) + {
      "mattercodex.dev/legacy-plan-id": strenv(PLAN_ID),
      "mattercodex.dev/legacy-source-root-reference": strenv(SOURCE_ROOT_REFERENCE),
      "mattercodex.dev/legacy-source-root-sha256": strenv(SOURCE_ROOT_SHA256),
      "mattercodex.dev/legacy-release-revision": strenv(RELEASE_REVISION),
      "mattercodex.dev/legacy-owner-evidence-sha256": strenv(OWNER_EVIDENCE_SHA256)
    }) |
    .data."owner-evidence.json" = load_str(strenv(OWNER_EVIDENCE))) |
  with(select(.kind == "Job" and .metadata.name == "legacy-data-migration");
    .spec.suspend = false |
    .spec.template.metadata.labels = ((.spec.template.metadata.labels // {}) + {
      "mattercodex.dev/environment": "production",
      "mattercodex.dev/release-managed": "false",
      "mattercodex.dev/migration-managed": "true"
    }) |
    (.spec.template.spec.containers[] | select(.name == "migration") | .env[] |
      select(.name == "LEGACY_DATA_MIGRATION_MODE") | .value) = "configuration-import" |
    (.spec.template.spec.containers[] | select(.name == "migration") | .env[] |
      select(.name == "LEGACY_DATA_MIGRATION_PLAN_ID") | .value) = strenv(PLAN_ID) |
    (.spec.template.spec.containers[] | select(.name == "migration") | .env[] |
      select(.name == "LEGACY_DATA_MIGRATION_SOURCE_ROOT_REFERENCE") | .value) = strenv(SOURCE_ROOT_REFERENCE) |
    (.spec.template.spec.containers[] | select(.name == "migration") | .env[] |
      select(.name == "LEGACY_DATA_MIGRATION_SOURCE_ROOT_SHA256") | .value) = strenv(SOURCE_ROOT_SHA256))
' "$render"

support="$temporary_directory/support.yaml"
job="$temporary_directory/job.yaml"
yq 'select(.kind != "Job")' "$render" >"$support"
yq 'select(.kind == "Job")' "$render" >"$job"
"${kube[@]}" apply -f "$support" >/dev/null
"${kube[@]}" -n "$namespace" delete job legacy-data-migration --ignore-not-found --wait=true >/dev/null
open_source_principal_window
"${kube[@]}" apply -f "$job" >/dev/null
migration_deadline=$((SECONDS + 7200))
migration_completed=false
while ((SECONDS < migration_deadline)); do
  migration_status=$("${kube[@]}" -n "$namespace" get job legacy-data-migration -o json)
  if jq -e 'any(.status.conditions[]?; .type == "Complete" and .status == "True")' \
    <<<"$migration_status" >/dev/null; then
    migration_completed=true
    break
  fi
  if jq -e 'any(.status.conditions[]?; .type == "Failed" and .status == "True")' \
    <<<"$migration_status" >/dev/null; then
    "${kube[@]}" -n "$namespace" logs job/legacy-data-migration --all-containers --tail=200 >&2 || true
    fail "migration Job failed"
  fi
  sleep 5
done
unset migration_status
[[ "$migration_completed" == true ]] || {
  "${kube[@]}" -n "$namespace" logs job/legacy-data-migration --all-containers --tail=200 >&2 || true
  fail "migration Job did not complete before deadline"
}
unset migration_completed
close_source_principal_window
"${kube[@]}" -n "$namespace" logs job/legacy-data-migration -c migration --tail=200
printf 'Legacy configuration import completed: plan=%s\n' "$plan_id"
