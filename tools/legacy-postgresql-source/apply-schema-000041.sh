#!/usr/bin/env bash

set -euo pipefail

readonly script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly repo_root="$(cd "${script_dir}/../.." && pwd)"
readonly migrations_dir="${repo_root}/services/external/bot-service/internal/repository/postgres/migrations"
readonly migration_file="${migrations_dir}/000041_legacy_data_cutover.sql"
readonly expected_migration_sha256=fe9c2dd37162233bc80a4351acedd246215e985b640df3fd36b746d4996beb0f
readonly namespace=matter-kodex-prod
readonly deployment=matter-codex-bot-service
readonly statefulset=mattermost-postgres

# shellcheck source=scripts/lib/env.sh
source "${repo_root}/scripts/lib/env.sh"

owner_approved=false
revision=""
bot_service_image=""
maintenance_window_id=""
max_outage_seconds=300
private_temporary_dir=""

cleanup() {
  [ -n "$private_temporary_dir" ] || return 0
  case "$private_temporary_dir" in /tmp/mattercodex-schema-000041-*) ;; *) mattercodex_die "отказ очищать неожиданный temporary path" ;; esac
  find "$private_temporary_dir" -type f -delete
  rmdir "$private_temporary_dir"
  private_temporary_dir=""
}
trap cleanup EXIT

while [ "$#" -gt 0 ]; do
  case "$1" in
    --owner-approved) owner_approved=true; shift ;;
    --revision) revision="${2:-}"; shift 2 ;;
    --bot-service-image) bot_service_image="${2:-}"; shift 2 ;;
    --maintenance-window-id) maintenance_window_id="${2:-}"; shift 2 ;;
    --max-outage-seconds) max_outage_seconds="${2:-}"; shift 2 ;;
    *) mattercodex_die "неизвестный аргумент: $1" ;;
  esac
done

validate() {
  [ "$owner_approved" = true ] || mattercodex_die "000041 запрещена без --owner-approved после merge и owner OK"
  [[ "$revision" =~ ^[a-f0-9]{40}$ ]] || mattercodex_die "--revision должен быть exact full Git SHA"
  [ "$(git -C "$repo_root" rev-parse HEAD)" = "$revision" ] || mattercodex_die "--revision не совпадает с checkout HEAD"
  [ -z "$(git -C "$repo_root" status --porcelain --untracked-files=all)" ] || mattercodex_die "требуется чистый exact checkout"
  [[ "$bot_service_image" =~ ^[^[:space:]]+@sha256:[a-f0-9]{64}$ ]] || mattercodex_die "--bot-service-image должен быть закреплён по sha256 digest"
  [[ "$maintenance_window_id" =~ ^[A-Za-z0-9][A-Za-z0-9._-]{2,63}$ ]] || mattercodex_die "требуется exact --maintenance-window-id"
  [[ "$max_outage_seconds" =~ ^[0-9]+$ ]] && [ "$max_outage_seconds" -ge 60 ] && [ "$max_outage_seconds" -le 600 ] ||
    mattercodex_die "--max-outage-seconds должен быть в диапазоне 60..600"
  mattercodex_require_commands jq kubectl sha256sum
}

admin_query() {
  kubectl exec -i --namespace "$namespace" "${statefulset}-0" --container postgres -- sh -ceu \
    'exec psql -X -v ON_ERROR_STOP=1 -At -U "$POSTGRES_USER" -d "$POSTGRES_DB"' <<< "$1"
}

schema_version() {
  admin_query "SELECT version_id FROM public.goose_db_version WHERE is_applied ORDER BY id DESC LIMIT 1;" 2>/dev/null
}

functional_checks() {
  kubectl exec --namespace "$namespace" "${statefulset}-0" --container postgres -- \
    sh -ceu 'exec pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB"' >/dev/null
  kubectl rollout status deployment/mattermost --namespace "$namespace" --timeout=60s >/dev/null
  kubectl rollout status "deployment/${deployment}" --namespace "$namespace" --timeout=60s >/dev/null
  kubectl get --raw "/api/v1/namespaces/${namespace}/services/http:mattermost:http/proxy/api/v4/system/ping" >/dev/null
  kubectl get --raw "/api/v1/namespaces/${namespace}/services/http:${deployment}:http/proxy/readyz" >/dev/null
}

inventory_preflight() {
  local count max_version actual_hash current_version expected=1 version applied_inventory
  count="$(find "$migrations_dir" -maxdepth 1 -type f -name '[0-9][0-9][0-9][0-9][0-9][0-9]_*.sql' | wc -l | tr -d '[:space:]')"
  max_version="$(find "$migrations_dir" -maxdepth 1 -type f -name '[0-9][0-9][0-9][0-9][0-9][0-9]_*.sql' -printf '%f\n' | sort | tail -1 | cut -d_ -f1)"
  [ "$count" = 41 ] && [ "$max_version" = 000041 ] || mattercodex_die "checkout содержит не exact 000001..000041 migration inventory"
  while IFS= read -r version; do
    [ "$((10#$version))" = "$expected" ] || mattercodex_die "checkout migration inventory содержит gap/duplicate"
    expected=$((expected + 1))
  done < <(find "$migrations_dir" -maxdepth 1 -type f -name '[0-9][0-9][0-9][0-9][0-9][0-9]_*.sql' -printf '%f\n' | sort | cut -d_ -f1)
  [ "$expected" = 42 ] || mattercodex_die "checkout migration inventory неполон"
  actual_hash="$(sha256sum "$migration_file" | awk '{print $1}')"
  [ "$actual_hash" = "$expected_migration_sha256" ] || mattercodex_die "000041 hash не совпадает с reviewed contract"
  current_version="$(schema_version)"
  [ "$current_version" = 40 ] || mattercodex_die "source goose version должна быть ровно 40; zero/multiple pending migrations отклонены"
  applied_inventory="$(admin_query "SELECT count(*) = 40 AND min(version_id) = 1 AND max(version_id) = 40 FROM (SELECT DISTINCT version_id FROM public.goose_db_version WHERE is_applied AND version_id > 0) AS applied;")"
  [ "$applied_inventory" = t ] || mattercodex_die "goose applied inventory не равен exact 000001..000040"
}

schema_readback() {
  local result
  [ "$(schema_version)" = 41 ] || mattercodex_die "goose version 41 не подтверждена"
  result="$(admin_query <<'SQL' 2>/dev/null
SELECT EXISTS (
           SELECT 1 FROM pg_catalog.pg_roles
           WHERE rolname = 'matter_codex_migration' AND NOT rolcanlogin AND NOT rolsuper
             AND NOT rolcreatedb AND NOT rolcreaterole AND NOT rolreplication AND NOT rolbypassrls
       )
       AND to_regclass('public.matter_codex_legacy_data_cutovers') IS NOT NULL
       AND to_regprocedure('public.matter_codex_legacy_snapshot_rows()') IS NOT NULL
       AND to_regprocedure('public.matter_codex_lock_legacy_business_tables()') IS NOT NULL
       AND has_table_privilege('matter_codex_migration', 'public.matter_codex_legacy_data_cutovers', 'SELECT,INSERT,UPDATE')
       AND NOT has_table_privilege('matter_codex_migration', 'public.matter_codex_legacy_data_cutovers', 'DELETE,TRUNCATE,REFERENCES,TRIGGER')
       AND has_schema_privilege('matter_codex_migration', 'public', 'USAGE')
       AND NOT has_schema_privilege('matter_codex_migration', 'public', 'CREATE');
SQL
)"
  [ "$result" = t ] || mattercodex_die "000041 schema/capability-role readback отклонён"
}

template_digest() {
  kubectl get deployment "$deployment" --namespace "$namespace" -o json | jq -cS '.spec.template' | sha256sum | awk '{print $1}'
}

restore_previous_template() {
  local record="$1" uid expected_uid observed_digest candidate_digest previous_digest snapshot patch_file
  expected_uid="$(kubectl get configmap "$record" --namespace "$namespace" -o 'go-template={{index .data "deployment-uid"}}')"
  uid="$(kubectl get deployment "$deployment" --namespace "$namespace" -o jsonpath='{.metadata.uid}')"
  [ "$uid" = "$expected_uid" ] || mattercodex_die "bot-service Deployment UID изменился; stale rollback отклонён"
  observed_digest="$(template_digest)"
  candidate_digest="$(kubectl get configmap "$record" --namespace "$namespace" -o 'go-template={{index .data "candidate-template-digest"}}')"
  previous_digest="$(kubectl get configmap "$record" --namespace "$namespace" -o 'go-template={{index .data "previous-template-digest"}}')"
  { [ "$observed_digest" = "$candidate_digest" ] || [ "$observed_digest" = "$previous_digest" ]; } ||
    mattercodex_die "bot-service PodTemplate не совпадает с exact migration ledger"
  [ "$observed_digest" = "$previous_digest" ] && return 0
  snapshot="${private_temporary_dir}/previous-template.json"
  patch_file="${private_temporary_dir}/restore.json"
  kubectl get configmap "$record" --namespace "$namespace" -o 'go-template={{index .data "previous-template.json"}}' > "$snapshot"
  jq -cn --slurpfile template "$snapshot" '[{"op":"replace","path":"/spec/template","value":$template[0]}]' > "$patch_file"
  kubectl patch deployment "$deployment" --namespace "$namespace" --type=json --patch-file "$patch_file" >/dev/null
  kubectl rollout status "deployment/${deployment}" --namespace "$namespace" --timeout="${max_outage_seconds}s" >/dev/null
}

apply_exact_000041() {
  local record current_image uid previous_template previous_digest candidate_template candidate_digest live_file patch_file
  functional_checks
  inventory_preflight
  umask 077
  private_temporary_dir="$(mktemp -d /tmp/mattercodex-schema-000041.XXXXXX)"
  record="mattercodex-schema-000041-${revision:0:12}"
  kubectl get configmap "$record" --namespace "$namespace" >/dev/null 2>&1 &&
    mattercodex_die "attempt для exact Git SHA уже существует; повтор требует нового reviewed revision"
  live_file="${private_temporary_dir}/deployment-live.json"
  previous_template="${private_temporary_dir}/previous-template.json"
  candidate_template="${private_temporary_dir}/candidate-template.json"
  patch_file="${private_temporary_dir}/candidate-patch.json"
  kubectl get deployment "$deployment" --namespace "$namespace" -o json > "$live_file"
  jq -cS '.spec.template' "$live_file" > "$previous_template"
  current_image="$(jq -r '.spec.template.spec.containers[] | select(.name=="bot-service") | .image' "$live_file")"
  uid="$(jq -r '.metadata.uid' "$live_file")"
  jq -cn --arg image "$bot_service_image" --arg revision "$revision" --arg window "$maintenance_window_id" \
    '{spec:{template:{metadata:{annotations:{"mattercodex.dev/schema-migration-revision":$revision,"mattercodex.dev/maintenance-window-id":$window}},spec:{containers:[{name:"bot-service",image:$image}]}}}}' > "$patch_file"
  kubectl patch --local -f "$live_file" --type=strategic --patch-file "$patch_file" -o json | jq -cS '.spec.template' > "$candidate_template"
  previous_digest="$(sha256sum "$previous_template" | awk '{print $1}')"
  candidate_digest="$(sha256sum "$candidate_template" | awk '{print $1}')"
  kubectl create configmap "$record" --namespace "$namespace" --from-file=previous-template.json="$previous_template" \
    --from-literal=state=PENDING --from-literal=source-git-revision="$revision" --from-literal=migration-version=000041 \
    --from-literal=migration-sha256="$expected_migration_sha256" --from-literal=maintenance-window-id="$maintenance_window_id" \
    --from-literal=deployment-uid="$uid" --from-literal=previous-image="$current_image" --from-literal=candidate-image="$bot_service_image" \
    --from-literal=previous-template-digest="$previous_digest" --from-literal=candidate-template-digest="$candidate_digest" \
    --dry-run=client -o yaml | kubectl create -f - >/dev/null
  kubectl patch configmap "$record" --namespace "$namespace" --type=merge --patch '{"immutable":true}' >/dev/null

  if ! (
    set -e
    kubectl patch deployment "$deployment" --namespace "$namespace" --type=strategic --patch-file "$patch_file" >/dev/null
    kubectl rollout status "deployment/${deployment}" --namespace "$namespace" --timeout="${max_outage_seconds}s" >/dev/null
    schema_readback
    functional_checks
  ); then
    restore_previous_template "$record"
    functional_checks
    if [ "$(schema_version)" = 41 ]; then schema_readback; fi
    mattercodex_die "000041 apply не принят; bot-service восстановлен, schema rollback запрещён как forward-only"
  fi
  kubectl create configmap "${record}-accepted" --namespace "$namespace" --from-literal=state=CURRENT \
    --from-literal=source-git-revision="$revision" --from-literal=applied-image="$bot_service_image" \
    --from-literal=applied-template-digest="$(template_digest)" --from-literal=goose-version=41 \
    --from-literal=schema-capability-readback=ok --dry-run=client -o yaml | kubectl create -f - >/dev/null
  kubectl patch configmap "${record}-accepted" --namespace "$namespace" --type=merge --patch '{"immutable":true}' >/dev/null
  mattercodex_log "exact 000041 применена штатным bot-service lifecycle; migration #196 не запускалась"
}

validate
apply_exact_000041
