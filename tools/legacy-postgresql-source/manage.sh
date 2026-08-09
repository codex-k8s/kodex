#!/usr/bin/env bash

set -euo pipefail

readonly script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly repo_root="$(cd "${script_dir}/../.." && pwd)"
readonly template_dir="${repo_root}/deploy/k8s/base/legacy-postgresql-source"
readonly canonical_readback_sql="${repo_root}/services/jobs/legacy-data-migration/internal/repository/postgres/sql/principal__readback.sql"
readonly principal_name=matter_codex_migration_g1
readonly credential_secret=legacy-data-migration-source-postgresql-g1
readonly trust_configmap=mattermost-postgresql-ca
readonly server_certificate=mattermost-postgres-migration-server-g1
readonly server_certificate_secret=mattermost-postgres-migration-server-g1
readonly statefulset_name=mattermost-postgres

# shellcheck source=scripts/lib/env.sh
source "${repo_root}/scripts/lib/env.sh"

command_name="${1:-}"
[ -n "$command_name" ] || mattercodex_die "укажите команду: render, preflight, apply, publish-client, readback или rollback"
shift

owner_approved=false
revision=""
render_dir="/tmp/mattercodex-legacy-postgresql-source-render"
env_file=""
readback_scope=source
private_temporary_dir=""

cleanup_private_temporary_dir() {
  [ -n "$private_temporary_dir" ] || return 0
  case "$private_temporary_dir" in
    /tmp/mattercodex-postgresql-*) ;;
    *) mattercodex_die "отказ очищать неожиданный temporary path" ;;
  esac
  find "$private_temporary_dir" -type f -delete
  rmdir "$private_temporary_dir"
  private_temporary_dir=""
}

trap cleanup_private_temporary_dir EXIT

while [ "$#" -gt 0 ]; do
  case "$1" in
    --owner-approved)
      owner_approved=true
      shift
      ;;
    --revision)
      revision="${2:-}"
      shift 2
      ;;
    --render-dir)
      render_dir="${2:-}"
      shift 2
      ;;
    --env-file)
      env_file="${2:-}"
      shift 2
      ;;
    --scope)
      readback_scope="${2:-}"
      shift 2
      ;;
    *)
      mattercodex_die "неизвестный аргумент: $1"
      ;;
  esac
done

if [ -n "$env_file" ]; then
  mattercodex_load_env_file "$env_file"
fi
mattercodex_set_defaults

export MATTERCODEX_LEGACY_POSTGRES_NAMESPACE="${MATTERCODEX_LEGACY_POSTGRES_NAMESPACE:-matter-kodex-prod}"
export MATTERCODEX_POSTGRES_CLIENT_NAMESPACE="${MATTERCODEX_POSTGRES_CLIENT_NAMESPACE:-mattercodex-system}"
export MATTERCODEX_POSTGRES_MIGRATION_TLS_SERVER_NAME="mattermost-postgres-migration.${MATTERCODEX_LEGACY_POSTGRES_NAMESPACE}.svc.cluster.local"
export MATTERCODEX_POSTGRES_MIGRATION_REVISION="${revision:-0000000000000000000000000000000000000000}"
export MATTERCODEX_POSTGRES_MIGRATION_CERTIFICATE_REVISION="${MATTERCODEX_POSTGRES_MIGRATION_CERTIFICATE_REVISION:-pending}"
export MATTERCODEX_POSTGRES_READBACK_NAMESPACE="${MATTERCODEX_LEGACY_POSTGRES_NAMESPACE}"

validate_inputs() {
  [[ "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]] ||
    mattercodex_die "некорректный source namespace"
  [[ "$MATTERCODEX_POSTGRES_CLIENT_NAMESPACE" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]] ||
    mattercodex_die "некорректный client namespace"
  [[ "$MATTERCODEX_POSTGRES_DB" =~ ^[A-Za-z_][A-Za-z0-9_-]{0,62}$ ]] ||
    mattercodex_die "некорректное имя PostgreSQL database"
  [[ "$MATTERCODEX_POSTGRES_IMAGE" =~ ^[^[:space:]]+@sha256:[a-f0-9]{64}$ ]] ||
    mattercodex_die "MATTERCODEX_POSTGRES_IMAGE должен быть закреплён по sha256 digest"
  case "$readback_scope" in
    source|client) ;;
    *) mattercodex_die "--scope поддерживает только source или client" ;;
  esac
  if [ -n "$revision" ] && [[ ! "$revision" =~ ^[a-f0-9]{40}$ ]]; then
    mattercodex_die "--revision должен быть полным Git SHA из 40 lowercase hex"
  fi
}

require_revision() {
  local checkout_revision
  [ -n "$revision" ] || mattercodex_die "для этой команды обязателен --revision с exact Git SHA"
  checkout_revision="$(git -C "$repo_root" rev-parse HEAD)"
  [ "$checkout_revision" = "$revision" ] || mattercodex_die "--revision не совпадает с exact checkout HEAD"
  [ -z "$(git -C "$repo_root" status --porcelain --untracked-files=all)" ] ||
    mattercodex_die "production-команда требует чистый exact checkout"
}

require_owner_gate() {
  [ "$owner_approved" = true ] ||
    mattercodex_die "production-изменение запрещено без явного --owner-approved после merge и owner OK"
}

render_template() {
  local source_file="$1"
  local target_file="$2"
  perl -pe '
    s/\$\{([A-Z][A-Z0-9_]*)\}/
      exists $ENV{$1} ? $ENV{$1} : die "missing template environment: $1\n"
    /gex
  ' "$source_file" > "$target_file"
}

render_readback_configmap() {
  local namespace="$1"
  local target_file="$2"
  if [ "$target_file" = - ]; then
    kubectl create configmap legacy-postgresql-source-readback \
      --namespace "$namespace" \
      --from-file=readback.sh="${script_dir}/readback.sh" \
      --from-file=principal__readback.sql="$canonical_readback_sql" \
      --dry-run=client \
      -o yaml
    return
  fi
  kubectl create configmap legacy-postgresql-source-readback \
    --namespace "$namespace" \
    --from-file=readback.sh="${script_dir}/readback.sh" \
    --from-file=principal__readback.sql="$canonical_readback_sql" \
    --dry-run=client \
    -o yaml > "$target_file"
}

render_all() {
  mattercodex_require_commands kubectl perl
  mkdir -p "$render_dir"
  render_template "${template_dir}/pki.yaml.tpl" "${render_dir}/pki.yaml"
  render_template "${template_dir}/runtime.yaml.tpl" "${render_dir}/runtime.yaml"
  render_template "${template_dir}/statefulset-patch.yaml.tpl" "${render_dir}/statefulset-patch.yaml"
  render_template "${template_dir}/client-runtime.yaml.tpl" "${render_dir}/client-runtime.yaml"
  render_template "${template_dir}/readback-job.yaml.tpl" "${render_dir}/readback-job.yaml"
  render_readback_configmap "$MATTERCODEX_POSTGRES_READBACK_NAMESPACE" "${render_dir}/readback-configmap.yaml"
  mattercodex_log "render legacy PostgreSQL source подготовлен"
}

kubectl_value() {
  local namespace="$1"
  local kind="$2"
  local name="$3"
  local jsonpath="$4"
  kubectl get "$kind" "$name" --namespace "$namespace" -o "jsonpath=${jsonpath}"
}

admin_query() {
  local query
  if [ "$#" -eq 1 ]; then
    query="$1"
  else
    query="$(cat)"
  fi
  kubectl exec -i \
    --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
    "${statefulset_name}-0" \
    --container postgres \
    -- sh -ceu 'exec psql -X -v ON_ERROR_STOP=1 -At -U "$POSTGRES_USER" -d "$POSTGRES_DB"' \
    <<< "$query"
}

preflight() {
  mattercodex_require_commands base64 grep kubectl
  kubectl get namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" >/dev/null
  kubectl get --raw=/readyz >/dev/null
  kubectl api-resources --api-group=cert-manager.io -o name | grep -qx certificates.cert-manager.io ||
    mattercodex_die "cert-manager Certificate API недоступен"
  kubectl auth can-i get secrets --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" | grep -qx yes ||
    mattercodex_die "нет read-доступа к source Secret metadata"
  kubectl auth can-i patch statefulsets.apps --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" | grep -qx yes ||
    mattercodex_die "нет права на code-first patch source StatefulSet"

  local statefulset_ready mattermost_ready bot_ready image actual_database schema_ready postgres_uid
  statefulset_ready="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" statefulset "$statefulset_name" '{.status.readyReplicas}')"
  mattermost_ready="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" deployment mattermost '{.status.availableReplicas}')"
  bot_ready="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" deployment matter-codex-bot-service '{.status.availableReplicas}')"
  [ "${statefulset_ready:-0}" = "1" ] || mattercodex_die "source PostgreSQL не Ready"
  [ "${mattermost_ready:-0}" = "1" ] || mattercodex_die "Mattermost не Ready"
  [ "${bot_ready:-0}" = "1" ] || mattercodex_die "legacy bot-service не Ready"
  if kubectl get namespace "$MATTERCODEX_POSTGRES_CLIENT_NAMESPACE" >/dev/null 2>&1; then
    if kubectl get job legacy-data-migration --namespace "$MATTERCODEX_POSTGRES_CLIENT_NAMESPACE" >/dev/null 2>&1; then
      [ "$(kubectl_value "$MATTERCODEX_POSTGRES_CLIENT_NAMESPACE" job legacy-data-migration '{.spec.suspend}')" = "true" ] ||
        mattercodex_die "legacy-data-migration Job не suspended"
      [ "$(kubectl_value "$MATTERCODEX_POSTGRES_CLIENT_NAMESPACE" job legacy-data-migration '{.status.active}')" != "1" ] ||
        mattercodex_die "legacy-data-migration Job уже активна"
    fi
  fi

  image="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" statefulset "$statefulset_name" '{.spec.template.spec.containers[?(@.name=="postgres")].image}')"
  [ "$image" = "$MATTERCODEX_POSTGRES_IMAGE" ] || mattercodex_die "source PostgreSQL image не совпадает с закреплённым manifest"
  postgres_uid="$(kubectl exec --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" "${statefulset_name}-0" --container postgres -- id -u postgres 2>/dev/null)"
  [ "$postgres_uid" = "999" ] || mattercodex_die "PostgreSQL runtime UID не соответствует TLS materializer owner"
  kubectl exec --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" "${statefulset_name}-0" --container postgres -- sh -ceu \
    'command -v bash >/dev/null && command -v openssl >/dev/null' ||
    mattercodex_die "закреплённый PostgreSQL image не содержит bash/openssl для TLS materializer"
  actual_database="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" secret "$MATTERCODEX_POSTGRES_SECRET" '{.data.postgres-db}' | base64 -d)"
  [ "$actual_database" = "$MATTERCODEX_POSTGRES_DB" ] || mattercodex_die "source database не совпадает с code-first настройкой"
  unset actual_database

  schema_ready="$(admin_query <<'SQL' 2>/dev/null
SELECT EXISTS (
           SELECT 1
           FROM pg_catalog.pg_roles
           WHERE rolname = 'matter_codex_migration'
             AND NOT rolcanlogin
             AND NOT rolsuper
             AND NOT rolcreatedb
             AND NOT rolcreaterole
             AND NOT rolreplication
             AND NOT rolbypassrls
       )
       AND to_regclass('public.matter_codex_legacy_data_cutovers') IS NOT NULL
       AND to_regprocedure('public.matter_codex_legacy_snapshot_rows()') IS NOT NULL
       AND to_regprocedure('public.matter_codex_lock_legacy_business_tables()') IS NOT NULL
       AND NOT EXISTS (
           SELECT 1
           FROM pg_catalog.pg_stat_activity
           WHERE usename = 'matter_codex_migration_g1'
             AND pid <> pg_catalog.pg_backend_pid()
       );
SQL
)"
  [ "$schema_ready" = "t" ] ||
    mattercodex_die "preparatory schema migration 000041 не применена; примените её отдельным штатным bot-service lifecycle до #241 apply"
  mattercodex_log "read-only source preflight: ok"
}

certificate_fingerprint() {
  kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" secret "$server_certificate_secret" '{.data.tls\.crt}' |
    base64 -d |
    openssl x509 -outform DER |
    sha256sum |
    awk '{print $1}'
}

sync_source_trust() {
  local temporary_dir ca_file server_file resource_version_before resource_version_after fingerprint
  umask 077
  temporary_dir="$(mktemp -d /tmp/mattercodex-postgresql-trust.XXXXXX)"
  private_temporary_dir="$temporary_dir"
  ca_file="${temporary_dir}/ca.pem"
  server_file="${temporary_dir}/server.pem"
  resource_version_before="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" secret "$server_certificate_secret" '{.metadata.resourceVersion}')"
  kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" secret "$server_certificate_secret" '{.data.ca\.crt}' | base64 -d > "$ca_file"
  kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" secret "$server_certificate_secret" '{.data.tls\.crt}' | base64 -d > "$server_file"
  resource_version_after="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" secret "$server_certificate_secret" '{.metadata.resourceVersion}')"
  [ "$resource_version_before" = "$resource_version_after" ] ||
    mattercodex_die "cert-manager Secret изменилась во время trust snapshot; повторите операцию"
  fingerprint="$(openssl x509 -in "$server_file" -outform DER | sha256sum | awk '{print $1}')"
  kubectl create configmap "$trust_configmap" \
    --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
    --from-file=ca.pem="$ca_file" \
    --from-file=server.pem="$server_file" \
    --dry-run=client -o yaml |
    kubectl apply --server-side --field-manager=mattercodex-legacy-postgresql-source -f - >/dev/null
  kubectl annotate configmap "$trust_configmap" \
    --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
    --overwrite \
    "mattercodex.dev/source-secret-resource-version=${resource_version_before}" \
    "mattercodex.dev/server-certificate-sha256=${fingerprint}" >/dev/null
  cleanup_private_temporary_dir
}

secret_value() {
  local namespace="$1"
  local key="$2"
  kubectl get secret "$credential_secret" --namespace "$namespace" \
    -o "go-template={{index .data \"${key}\"}}" | base64 -d
}

principal_exists() {
  [ "$(admin_query "SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = '${principal_name}');" 2>/dev/null)" = "t" ]
}

ensure_source_credential() {
  local secret_exists=false role_exists=false password database source_dsn temporary_dir
  if kubectl get secret "$credential_secret" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" >/dev/null 2>&1; then
    secret_exists=true
  fi
  if principal_exists; then
    role_exists=true
  fi
  if [ "$role_exists" = true ] && [ "$secret_exists" = false ]; then
    mattercodex_die "migration principal существует без code-first credential Secret; автоматическая замена credential запрещена"
  fi

  database="$MATTERCODEX_POSTGRES_DB"
  if [ "$secret_exists" = true ]; then
    [ "$(secret_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" username)" = "$principal_name" ] ||
      mattercodex_die "существующий credential Secret содержит другой principal"
    [ "$(secret_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" database)" = "$database" ] ||
      mattercodex_die "существующий credential Secret содержит другую database"
    password="$(secret_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" password)"
    source_dsn="$(secret_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" source-dsn)"
    [[ "$password" =~ ^[a-f0-9]{64}$ ]] || mattercodex_die "существующий password не соответствует lifecycle contract"
    [ "$source_dsn" = "postgres://${principal_name}:${password}@${MATTERCODEX_POSTGRES_MIGRATION_TLS_SERVER_NAME}:5432/${database}?sslmode=verify-full&connect_timeout=10" ] ||
      mattercodex_die "существующий source DSN не соответствует exact TLS contract"
  else
    password="$(openssl rand -hex 32)"
    source_dsn="postgres://${principal_name}:${password}@${MATTERCODEX_POSTGRES_MIGRATION_TLS_SERVER_NAME}:5432/${database}?sslmode=verify-full&connect_timeout=10"
    umask 077
    temporary_dir="$(mktemp -d /tmp/mattercodex-postgresql-credential.XXXXXX)"
    private_temporary_dir="$temporary_dir"
    printf '%s' "$principal_name" > "${temporary_dir}/username"
    printf '%s' "$password" > "${temporary_dir}/password"
    printf '%s' "$database" > "${temporary_dir}/database"
    printf '%s' "$source_dsn" > "${temporary_dir}/source-dsn"
    kubectl create secret generic "$credential_secret" \
      --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
      --from-file=username="${temporary_dir}/username" \
      --from-file=password="${temporary_dir}/password" \
      --from-file=database="${temporary_dir}/database" \
      --from-file=source-dsn="${temporary_dir}/source-dsn" \
      --dry-run=client -o yaml |
      kubectl create -f - >/dev/null
    cleanup_private_temporary_dir
  fi
  kubectl patch secret "$credential_secret" \
    --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
    --type=merge \
    --patch '{"immutable":true}' >/dev/null

  {
    printf "\\set migration_password '%s'\n" "$password"
    sed '/^-- name:/d' "${script_dir}/sql/bootstrap-principal.sql"
  } | kubectl exec -i \
    --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
    "${statefulset_name}-0" \
    --container postgres \
    -- sh -ceu 'exec psql -X -q -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"' >/dev/null
  unset password source_dsn
}

enable_principal() {
  sed '/^-- name:/d' "${script_dir}/sql/principal-login.sql" | kubectl exec -i \
    --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
    "${statefulset_name}-0" --container postgres \
    -- sh -ceu 'exec psql -X -q -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"' >/dev/null
}

record_previous_revision() {
  local record_name previous_name previous_number existing_revision
  record_name="mattermost-postgres-migration-rollout-${revision:0:12}"
  if kubectl get configmap "$record_name" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" >/dev/null 2>&1; then
    existing_revision="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" configmap "$record_name" '{.data.source-git-revision}')"
    [ "$existing_revision" = "$revision" ] || mattercodex_die "rollout record занят другой Git revision"
    return
  fi
  previous_name="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" statefulset "$statefulset_name" '{.status.currentRevision}')"
  [ -n "$previous_name" ] || mattercodex_die "StatefulSet currentRevision не определена"
  previous_number="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" controllerrevision "$previous_name" '{.revision}')"
  [[ "$previous_number" =~ ^[0-9]+$ ]] || mattercodex_die "StatefulSet revision number не определён"
  kubectl create configmap "$record_name" \
    --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
    --from-literal=source-git-revision="$revision" \
    --from-literal=previous-controller-revision-name="$previous_name" \
    --from-literal=previous-controller-revision-number="$previous_number" \
    --from-literal=certificate-revision="$MATTERCODEX_POSTGRES_MIGRATION_CERTIFICATE_REVISION" \
    --dry-run=client -o yaml |
    kubectl create -f - >/dev/null
  kubectl patch configmap "$record_name" \
    --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
    --type=merge \
    --patch '{"immutable":true}' >/dev/null
}

apply_readback_configmap() {
  local namespace="$1"
  render_readback_configmap "$namespace" - |
    kubectl apply --server-side --field-manager=mattercodex-legacy-postgresql-source -f - >/dev/null
}

run_readback() {
  local namespace="$1" job_name expected_fingerprint
  expected_fingerprint="$(kubectl get configmap "$trust_configmap" --namespace "$namespace" -o 'go-template={{index .metadata.annotations "mattercodex.dev/server-certificate-sha256"}}')"
  [[ "$expected_fingerprint" =~ ^[a-f0-9]{64}$ ]] || mattercodex_die "trust ConfigMap не содержит exact certificate fingerprint"
  [ "$(certificate_fingerprint)" = "$expected_fingerprint" ] ||
    mattercodex_die "cert-manager leaf изменился после trust publication"
  export MATTERCODEX_POSTGRES_READBACK_NAMESPACE="$namespace"
  render_template "${template_dir}/readback-job.yaml.tpl" "${render_dir}/readback-job.yaml"
  job_name="$(kubectl create -f "${render_dir}/readback-job.yaml" -o name)"
  if ! kubectl wait --namespace "$namespace" --for=condition=complete --timeout=150s "$job_name" >/dev/null; then
    kubectl logs --namespace "$namespace" "$job_name" --container readback --tail=40 >&2 || true
    mattercodex_die "code-first readback Job завершилась неуспешно"
  fi
  kubectl logs --namespace "$namespace" "$job_name" --container readback --tail=1 | grep -qx 'legacy PostgreSQL source readback: ok' ||
    mattercodex_die "readback Job не вернула фиксированный success marker"
  [ "$(certificate_fingerprint)" = "$expected_fingerprint" ] ||
    mattercodex_die "cert-manager leaf изменился во время served-state readback"
  mattercodex_log "served-state TLS и effective privilege readback: ok"
}

apply_source() {
  require_owner_gate
  require_revision
  preflight
  mattercodex_require_commands base64 kubectl openssl perl sha256sum
  export MATTERCODEX_POSTGRES_READBACK_NAMESPACE="$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE"
  render_all
  kubectl apply -f "${render_dir}/pki.yaml" >/dev/null
  kubectl wait --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
    --for=condition=Ready --timeout=180s certificate/mattermost-postgres-migration-ca-g1 >/dev/null
  kubectl wait --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
    --for=condition=Ready --timeout=180s "certificate/${server_certificate}" >/dev/null
  sync_source_trust
  MATTERCODEX_POSTGRES_MIGRATION_CERTIFICATE_REVISION="$(certificate_fingerprint)"
  export MATTERCODEX_POSTGRES_MIGRATION_CERTIFICATE_REVISION
  render_all
  kubectl apply -f "${render_dir}/runtime.yaml" >/dev/null
  kubectl apply --server-side --field-manager=mattercodex-legacy-postgresql-source \
    -f "${render_dir}/readback-configmap.yaml" >/dev/null
  ensure_source_credential
  record_previous_revision
  kubectl patch statefulset "$statefulset_name" \
    --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
    --type=strategic \
    --patch-file "${render_dir}/statefulset-patch.yaml" >/dev/null
  kubectl rollout status statefulset "$statefulset_name" \
    --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" --timeout=300s >/dev/null
  kubectl rollout status deployment mattermost \
    --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" --timeout=180s >/dev/null
  kubectl rollout status deployment matter-codex-bot-service \
    --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" --timeout=180s >/dev/null
  enable_principal
  if ! (run_readback "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE"); then
    retire_principal
    mattercodex_die "served-state readback отклонён; migration principal возвращён в NOLOGIN"
  fi
  mattercodex_log "legacy PostgreSQL native TLS source применён; migration Job не запускалась"
}

copy_trust_to_client_namespace() {
  kubectl get configmap "$trust_configmap" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" -o json |
    jq --arg namespace "$MATTERCODEX_POSTGRES_CLIENT_NAMESPACE" '
      del(
        .metadata.creationTimestamp,
        .metadata.managedFields,
        .metadata.ownerReferences,
        .metadata.resourceVersion,
        .metadata.uid
      )
      | .metadata.namespace = $namespace
    ' |
    kubectl apply --server-side --field-manager=mattercodex-legacy-postgresql-source -f - >/dev/null
}

copy_credential_to_client_namespace() {
  local source_digest target_digest
  if kubectl get secret "$credential_secret" --namespace "$MATTERCODEX_POSTGRES_CLIENT_NAMESPACE" >/dev/null 2>&1; then
    source_digest="$(kubectl get secret "$credential_secret" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" -o json | jq -cS '.data' | sha256sum | awk '{print $1}')"
    target_digest="$(kubectl get secret "$credential_secret" --namespace "$MATTERCODEX_POSTGRES_CLIENT_NAMESPACE" -o json | jq -cS '.data' | sha256sum | awk '{print $1}')"
    [ "$source_digest" = "$target_digest" ] || mattercodex_die "client credential Secret существует с другим immutable content"
    kubectl patch secret "$credential_secret" \
      --namespace "$MATTERCODEX_POSTGRES_CLIENT_NAMESPACE" \
      --type=merge --patch '{"immutable":true}' >/dev/null
    return
  fi
  kubectl get secret "$credential_secret" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" -o json |
    jq --arg namespace "$MATTERCODEX_POSTGRES_CLIENT_NAMESPACE" '
      del(
        .metadata.annotations,
        .metadata.creationTimestamp,
        .metadata.managedFields,
        .metadata.ownerReferences,
        .metadata.resourceVersion,
        .metadata.uid
      )
      | .metadata.namespace = $namespace
    ' |
    kubectl create -f - >/dev/null
}

publish_client() {
  require_owner_gate
  require_revision
  mattercodex_require_commands jq kubectl perl
  kubectl get namespace "$MATTERCODEX_POSTGRES_CLIENT_NAMESPACE" >/dev/null ||
    mattercodex_die "client namespace ещё не создан; сначала завершите отдельную wave #197"
  kubectl get service mattermost-postgres-migration --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" >/dev/null
  kubectl get secret "$credential_secret" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" >/dev/null
  kubectl get configmap "$trust_configmap" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" >/dev/null
  sync_source_trust
  export MATTERCODEX_POSTGRES_READBACK_NAMESPACE="$MATTERCODEX_POSTGRES_CLIENT_NAMESPACE"
  render_all
  kubectl apply -f "${render_dir}/client-runtime.yaml" >/dev/null
  apply_readback_configmap "$MATTERCODEX_POSTGRES_CLIENT_NAMESPACE"
  copy_credential_to_client_namespace
  copy_trust_to_client_namespace
  if ! (run_readback "$MATTERCODEX_POSTGRES_CLIENT_NAMESPACE"); then
    kubectl delete secret "$credential_secret" \
      --namespace "$MATTERCODEX_POSTGRES_CLIENT_NAMESPACE" --ignore-not-found >/dev/null
    mattercodex_die "client readback отклонён; опубликованный client credential удалён"
  fi
  mattercodex_log "client TLS credential/trust опубликованы после successful readback"
}

retire_principal() {
  sed '/^-- name:/d' "${script_dir}/sql/principal-no-login.sql" | kubectl exec -i \
    --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
    "${statefulset_name}-0" --container postgres \
    -- sh -ceu 'exec psql -X -q -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"' >/dev/null
  sed '/^-- name:/d' "${script_dir}/sql/principal-terminate-sessions.sql" | kubectl exec -i \
    --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
    "${statefulset_name}-0" --container postgres \
    -- sh -ceu 'exec psql -X -q -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"' >/dev/null
}

rollback_source() {
  require_owner_gate
  require_revision
  local record_name previous_number observed_revision
  record_name="mattermost-postgres-migration-rollout-${revision:0:12}"
  kubectl get configmap "$record_name" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" >/dev/null ||
    mattercodex_die "rollback record для exact Git revision не найден"
  [ "$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" configmap "$record_name" '{.data.source-git-revision}')" = "$revision" ] ||
    mattercodex_die "rollback record не совпадает с requested Git revision"
  previous_number="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" configmap "$record_name" '{.data.previous-controller-revision-number}')"
  [[ "$previous_number" =~ ^[0-9]+$ ]] || mattercodex_die "previous ControllerRevision number отсутствует"
  retire_principal
  kubectl rollout undo statefulset "$statefulset_name" \
    --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
    --to-revision="$previous_number" >/dev/null
  kubectl rollout status statefulset "$statefulset_name" \
    --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" --timeout=300s >/dev/null
  kubectl delete -f "${render_dir}/runtime.yaml" --ignore-not-found >/dev/null
  kubectl rollout status deployment mattermost \
    --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" --timeout=180s >/dev/null
  kubectl rollout status deployment matter-codex-bot-service \
    --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" --timeout=180s >/dev/null
  observed_revision="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" statefulset "$statefulset_name" '{.status.currentRevision}')"
  kubectl annotate configmap "$record_name" \
    --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
    --overwrite "mattercodex.dev/rollback-observed-controller-revision=${observed_revision}" >/dev/null
  mattercodex_log "rollback exact previous ControllerRevision выполнен; principal переведён в NOLOGIN"
}

validate_inputs

case "$command_name" in
  render)
    render_all
    ;;
  preflight)
    preflight
    ;;
  apply)
    apply_source
    ;;
  publish-client)
    publish_client
    ;;
  readback)
    require_owner_gate
    require_revision
    if [ "$readback_scope" = source ]; then
      export MATTERCODEX_POSTGRES_READBACK_NAMESPACE="$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE"
    else
      export MATTERCODEX_POSTGRES_READBACK_NAMESPACE="$MATTERCODEX_POSTGRES_CLIENT_NAMESPACE"
    fi
    render_all
    sync_source_trust
    apply_readback_configmap "$MATTERCODEX_POSTGRES_READBACK_NAMESPACE"
    if [ "$readback_scope" = client ]; then
      copy_trust_to_client_namespace
    fi
    run_readback "$MATTERCODEX_POSTGRES_READBACK_NAMESPACE"
    ;;
  rollback)
    require_owner_gate
    require_revision
    export MATTERCODEX_POSTGRES_READBACK_NAMESPACE="$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE"
    render_all
    rollback_source
    ;;
  *)
    mattercodex_die "неизвестная команда: $command_name"
    ;;
esac
