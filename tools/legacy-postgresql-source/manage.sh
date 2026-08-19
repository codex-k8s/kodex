#!/usr/bin/env bash

set -euo pipefail

readonly script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly repo_root="$(cd "${script_dir}/../.." && pwd)"
readonly template_dir="${repo_root}/deploy/k8s/base/legacy-postgresql-source"
readonly canonical_readback_sql="${repo_root}/services/jobs/legacy-data-migration/internal/repository/postgres/sql/principal__readback.sql"
readonly principal_name=matter_codex_migration_g1
readonly capability_role=matter_codex_migration
readonly credential_secret=legacy-data-migration-source-postgresql-g1
readonly ca_secret=mattermost-postgres-migration-ca-g1
readonly ca_record=mattermost-postgres-migration-ca-g1-record
readonly server_certificate=mattermost-postgres-migration-server-g1
readonly server_certificate_secret=mattermost-postgres-migration-server-g1
readonly rollout_index=mattermost-postgres-migration-rollout-index
readonly statefulset_name=mattermost-postgres
readonly client_ingress=mattermost-postgres-migration-client-ingress

# shellcheck source=scripts/lib/env.sh
source "${repo_root}/scripts/lib/env.sh"

command_name="${1:-}"
[ -n "$command_name" ] || mattercodex_die "укажите команду: render, preflight, apply, renew, publish-client, readback или rollback"
shift

owner_approved=false
revision=""
requested_attempt=""
maintenance_window_id=""
max_outage_seconds=300
render_dir=/tmp/mattercodex-legacy-postgresql-source-render
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
    --attempt)
      requested_attempt="${2:-}"
      shift 2
      ;;
    --maintenance-window-id)
      maintenance_window_id="${2:-}"
      shift 2
      ;;
    --max-outage-seconds)
      max_outage_seconds="${2:-}"
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
export MATTERCODEX_POSTGRES_MIGRATION_CERTIFICATE_REVISION=pending
export MATTERCODEX_POSTGRES_ROLLOUT_ATTEMPT=00000000000000000000
export MATTERCODEX_POSTGRES_RUNTIME_SECRET=mattermost-postgres-migration-runtime-pending
export MATTERCODEX_POSTGRES_ACTIVATION_CONFIGMAP=mattermost-postgres-migration-activation-pending
export MATTERCODEX_POSTGRES_READBACK_TRUST_CONFIGMAP=mattermost-postgres-migration-trust-pending
export MATTERCODEX_POSTGRES_READBACK_NAMESPACE="$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE"

validate_inputs() {
  [[ "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]] ||
    mattercodex_die "некорректный source namespace"
  [[ "$MATTERCODEX_POSTGRES_CLIENT_NAMESPACE" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]] ||
    mattercodex_die "некорректный client namespace"
  [[ "$MATTERCODEX_POSTGRES_DB" =~ ^[A-Za-z_][A-Za-z0-9_-]{0,62}$ ]] ||
    mattercodex_die "некорректное имя PostgreSQL database"
  [[ "$MATTERCODEX_POSTGRES_IMAGE" =~ ^[^[:space:]]+@sha256:[a-f0-9]{64}$ ]] ||
    mattercodex_die "MATTERCODEX_POSTGRES_IMAGE должен быть закреплён по sha256 digest"
  [[ "$max_outage_seconds" =~ ^[0-9]+$ ]] || mattercodex_die "--max-outage-seconds должен быть целым числом"
  [ "$max_outage_seconds" -ge 60 ] && [ "$max_outage_seconds" -le 600 ] ||
    mattercodex_die "--max-outage-seconds должен быть в диапазоне 60..600"
  case "$readback_scope" in source|client) ;; *) mattercodex_die "--scope поддерживает только source или client" ;; esac
  if [ -n "$revision" ] && [[ ! "$revision" =~ ^[a-f0-9]{40}$ ]]; then
    mattercodex_die "--revision должен быть полным Git SHA из 40 lowercase hex"
  fi
  if [ -n "$requested_attempt" ] && [[ ! "$requested_attempt" =~ ^[0-9]{20}$ ]]; then
    mattercodex_die "--attempt должен быть 20-значным monotonic identity"
  fi
  if [ -n "$maintenance_window_id" ] && [[ ! "$maintenance_window_id" =~ ^[A-Za-z0-9][A-Za-z0-9._-]{2,63}$ ]]; then
    mattercodex_die "--maintenance-window-id имеет некорректный формат"
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
  [ -n "$maintenance_window_id" ] ||
    mattercodex_die "production-изменение требует exact --maintenance-window-id"
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
    kubectl create configmap legacy-postgresql-source-readback --namespace "$namespace" \
      --from-file=readback.sh="${script_dir}/readback.sh" \
      --from-file=principal__readback.sql="$canonical_readback_sql" --dry-run=client -o yaml
    return
  fi
  kubectl create configmap legacy-postgresql-source-readback --namespace "$namespace" \
    --from-file=readback.sh="${script_dir}/readback.sh" \
    --from-file=principal__readback.sql="$canonical_readback_sql" --dry-run=client -o yaml > "$target_file"
}

render_all() {
  mattercodex_require_commands kubectl perl
  mkdir -p "$render_dir"
  render_template "${template_dir}/pki.yaml.tpl" "${render_dir}/pki.yaml"
  render_template "${template_dir}/runtime.yaml.tpl" "${render_dir}/runtime.yaml"
  render_template "${template_dir}/client-ingress.yaml.tpl" "${render_dir}/client-ingress.yaml"
  render_template "${template_dir}/statefulset-patch.yaml.tpl" "${render_dir}/statefulset-patch.yaml"
  render_template "${template_dir}/client-runtime.yaml.tpl" "${render_dir}/client-runtime.yaml"
  render_template "${template_dir}/readback-job.yaml.tpl" "${render_dir}/readback-job.yaml"
  render_readback_configmap "$MATTERCODEX_POSTGRES_READBACK_NAMESPACE" "${render_dir}/readback-configmap.yaml"
  mattercodex_log "render legacy PostgreSQL source подготовлен"
}

kubectl_value() {
  local namespace="$1" kind="$2" name="$3" jsonpath="$4"
  kubectl get "$kind" "$name" --namespace "$namespace" -o "jsonpath=${jsonpath}"
}

admin_query() {
  local query
  if [ "$#" -eq 1 ]; then query="$1"; else query="$(sed -n '1,$p')"; fi
  kubectl exec -i --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" "${statefulset_name}-0" \
    --container postgres -- sh -ceu \
    'exec psql -X -v ON_ERROR_STOP=1 -At -U "$POSTGRES_USER" -d "$POSTGRES_DB"' <<< "$query"
}

preflight() {
  mattercodex_require_commands base64 curl grep jq kubectl openssl perl sha256sum
  kubectl get namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" >/dev/null
  kubectl get --raw=/readyz >/dev/null
  kubectl api-resources --api-group=cert-manager.io -o name | grep -qx certificates.cert-manager.io ||
    mattercodex_die "cert-manager Certificate API недоступен"
  kubectl auth can-i get secrets --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" | grep -qx yes ||
    mattercodex_die "нет read-доступа к source Secret metadata"
  kubectl auth can-i patch statefulsets.apps --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" | grep -qx yes ||
    mattercodex_die "нет права на code-first patch source StatefulSet"

  local image actual_database schema_ready postgres_uid
  [ "$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" statefulset "$statefulset_name" '{.status.readyReplicas}')" = 1 ] ||
    mattercodex_die "source PostgreSQL не Ready"
  [ "$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" deployment mattermost '{.status.availableReplicas}')" = 1 ] ||
    mattercodex_die "Mattermost не Ready"
  [ "$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" deployment matter-codex-bot-service '{.status.availableReplicas}')" = 1 ] ||
    mattercodex_die "legacy bot-service не Ready"
  if kubectl get namespace "$MATTERCODEX_POSTGRES_CLIENT_NAMESPACE" >/dev/null 2>&1 &&
     kubectl get job legacy-data-migration --namespace "$MATTERCODEX_POSTGRES_CLIENT_NAMESPACE" >/dev/null 2>&1; then
    [ "$(kubectl_value "$MATTERCODEX_POSTGRES_CLIENT_NAMESPACE" job legacy-data-migration '{.spec.suspend}')" = true ] ||
      mattercodex_die "legacy-data-migration Job не suspended"
    [ "$(kubectl_value "$MATTERCODEX_POSTGRES_CLIENT_NAMESPACE" job legacy-data-migration '{.status.active}')" != 1 ] ||
      mattercodex_die "legacy-data-migration Job уже активна"
  fi

  image="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" statefulset "$statefulset_name" '{.spec.template.spec.containers[?(@.name=="postgres")].image}')"
  [ "$image" = "$MATTERCODEX_POSTGRES_IMAGE" ] || mattercodex_die "source PostgreSQL image не совпадает с закреплённым manifest"
  postgres_uid="$(kubectl exec --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" "${statefulset_name}-0" --container postgres -- id -u postgres 2>/dev/null)"
  [ "$postgres_uid" = 999 ] || mattercodex_die "PostgreSQL runtime UID не соответствует TLS materializer owner"
  kubectl exec --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" "${statefulset_name}-0" --container postgres -- \
    sh -ceu 'command -v bash >/dev/null && command -v openssl >/dev/null && command -v timeout >/dev/null' ||
    mattercodex_die "закреплённый PostgreSQL image не содержит bash/openssl/timeout для TLS materializer/readback"
  actual_database="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" secret "$MATTERCODEX_POSTGRES_SECRET" '{.data.postgres-db}' | base64 -d)"
  [ "$actual_database" = "$MATTERCODEX_POSTGRES_DB" ] || mattercodex_die "source database не совпадает с code-first настройкой"
  unset actual_database

  schema_ready="$(admin_query <<'SQL' 2>/dev/null
SELECT EXISTS (
           SELECT 1 FROM pg_catalog.pg_roles
           WHERE rolname = 'matter_codex_migration' AND NOT rolcanlogin AND NOT rolsuper
             AND NOT rolcreatedb AND NOT rolcreaterole AND NOT rolreplication AND NOT rolbypassrls
       )
       AND to_regclass('public.matter_codex_legacy_data_cutovers') IS NOT NULL
       AND to_regprocedure('public.matter_codex_legacy_snapshot_rows()') IS NOT NULL
       AND to_regprocedure('public.matter_codex_lock_legacy_business_tables()') IS NOT NULL;
SQL
)"
  [ "$schema_ready" = t ] ||
    mattercodex_die "preparatory schema migration 000041 не применена; сначала используйте exact apply-schema-000041.sh"
  mattercodex_log "read-only source preflight: ok"
}

functional_checks() {
  kubectl exec --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" "${statefulset_name}-0" --container postgres -- \
    sh -ceu 'exec pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB"' >/dev/null
  kubectl rollout status deployment/mattermost --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" --timeout=60s >/dev/null
  kubectl rollout status deployment/matter-codex-bot-service --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" --timeout=60s >/dev/null
  kubectl get --raw "/api/v1/namespaces/${MATTERCODEX_LEGACY_POSTGRES_NAMESPACE}/services/http:mattermost:http/proxy/api/v4/system/ping" >/dev/null
  kubectl get --raw "/api/v1/namespaces/${MATTERCODEX_LEGACY_POSTGRES_NAMESPACE}/services/http:matter-codex-bot-service:http/proxy/readyz" >/dev/null
  mattercodex_log "maintenance functional checks: PostgreSQL, Mattermost и bot-service ok"
}

ensure_rollout_index() {
  if kubectl get configmap "$rollout_index" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" >/dev/null 2>&1; then return; fi
  kubectl create configmap "$rollout_index" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
    --from-literal=next-attempt=1 --from-literal=pending-attempt= --from-literal=current-attempt= \
    --from-literal=current-statefulset-uid= --from-literal=current-template-digest= \
    --from-literal=current-certificate-fingerprint= --dry-run=client -o yaml | kubectl create -f - >/dev/null
}

index_value() {
  kubectl get configmap "$rollout_index" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
    -o "go-template={{index .data \"$1\"}}"
}

template_digest() {
  kubectl get statefulset "$statefulset_name" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" -o json |
    jq -cS '.spec.template' | sha256sum | awk '{print $1}'
}

served_fingerprint() {
  kubectl exec --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" "${statefulset_name}-0" --container postgres -- \
    bash -ceu '
      host="$1"
      openssl s_client -starttls postgres -connect "${host}:5432" -servername "$host" \
        -verify_hostname "$host" -verify_return_error -CAfile /var/run/postgresql-migration-tls/ca.crt \
        -tls1_3 -showcerts < /dev/null 2>/dev/null |
        awk "/-----BEGIN CERTIFICATE-----/{capture=1} capture{print} /-----END CERTIFICATE-----/{exit}" |
        openssl x509 -outform DER |
        sha256sum |
        awk "{print \$1}"
    ' -- "$MATTERCODEX_POSTGRES_MIGRATION_TLS_SERVER_NAME"
}

allocate_attempt() {
  local rv next attempt patch
  rv="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" configmap "$rollout_index" '{.metadata.resourceVersion}')"
  [ -z "$(index_value pending-attempt)" ] || mattercodex_die "обнаружена незавершённая rollout attempt; требуется fail-closed recovery"
  next="$(index_value next-attempt)"
  [[ "$next" =~ ^[0-9]+$ ]] || mattercodex_die "rollout index повреждён"
  printf -v attempt '%020d' "$next"
  patch="$(jq -cn --arg rv "$rv" --arg next "$((next + 1))" \
    '{metadata:{resourceVersion:$rv},data:{"next-attempt":$next}}')"
  kubectl patch configmap "$rollout_index" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" --type=merge --patch "$patch" >/dev/null
  printf '%s' "$attempt"
}

mark_attempt_pending() {
  local attempt="$1" rv patch
  [ -z "$(index_value pending-attempt)" ] || mattercodex_die "parallel rollout attempt обнаружена"
  rv="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" configmap "$rollout_index" '{.metadata.resourceVersion}')"
  patch="$(jq -cn --arg rv "$rv" --arg attempt "$attempt" \
    '{metadata:{resourceVersion:$rv},data:{"pending-attempt":$attempt}}')"
  kubectl patch configmap "$rollout_index" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" --type=merge --patch "$patch" >/dev/null
}

ca_fingerprint() {
  kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" secret "$ca_secret" '{.data.tls\.crt}' |
    base64 -d | openssl x509 -outform DER | sha256sum | awk '{print $1}'
}

ensure_ca_generation() {
  local temporary_dir key_file cert_file fingerprint uid rv
  if ! kubectl get secret "$ca_secret" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" >/dev/null 2>&1; then
    umask 077
    temporary_dir="$(mktemp -d /tmp/mattercodex-postgresql-ca.XXXXXX)"
    private_temporary_dir="$temporary_dir"
    key_file="${temporary_dir}/tls.key"
    cert_file="${temporary_dir}/tls.crt"
    openssl ecparam -name prime256v1 -genkey -noout -out "$key_file"
    openssl req -x509 -new -sha384 -key "$key_file" -days 3650 -subj /CN=mattermost-postgres-migration-ca-g1 \
      -addext basicConstraints=critical,CA:TRUE,pathlen:0 \
      -addext keyUsage=critical,keyCertSign,cRLSign -out "$cert_file"
    kubectl create secret tls "$ca_secret" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
      --cert="$cert_file" --key="$key_file" --dry-run=client -o yaml | kubectl create -f - >/dev/null
    kubectl patch secret "$ca_secret" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
      --type=merge --patch '{"immutable":true}' >/dev/null
    cleanup_private_temporary_dir
  fi
  [ "$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" secret "$ca_secret" '{.immutable}')" = true ] ||
    mattercodex_die "CA g1 Secret должна быть immutable"
  fingerprint="$(ca_fingerprint)"
  uid="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" secret "$ca_secret" '{.metadata.uid}')"
  rv="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" secret "$ca_secret" '{.metadata.resourceVersion}')"
  if kubectl get configmap "$ca_record" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" >/dev/null 2>&1; then
    [ "$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" configmap "$ca_record" '{.data.ca-fingerprint}')" = "$fingerprint" ] ||
      mattercodex_die "CA g1 fingerprint не совпадает с immutable generation record"
    [ "$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" configmap "$ca_record" '{.data.ca-secret-uid}')" = "$uid" ] ||
      mattercodex_die "CA g1 Secret была пересоздана; требуется отдельный g2 overlap PR"
    return
  fi
  kubectl create configmap "$ca_record" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
    --from-literal=generation=g1 --from-literal=ca-fingerprint="$fingerprint" \
    --from-literal=ca-secret-uid="$uid" --from-literal=ca-secret-resource-version="$rv" \
    --dry-run=client -o yaml | kubectl create -f - >/dev/null
  kubectl patch configmap "$ca_record" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
    --type=merge --patch '{"immutable":true}' >/dev/null
}

snapshot_candidate_leaf() {
  local temporary_dir ca_file cert_file key_file before after cert_generation fingerprint short runtime_secret trust_map content_digest
  umask 077
  temporary_dir="$(mktemp -d /tmp/mattercodex-postgresql-leaf.XXXXXX)"
  private_temporary_dir="$temporary_dir"
  ca_file="${temporary_dir}/ca.pem"
  cert_file="${temporary_dir}/server.pem"
  key_file="${temporary_dir}/server.key"
  before="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" secret "$server_certificate_secret" '{.metadata.resourceVersion}')"
  cert_generation="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" certificate "$server_certificate" '{.metadata.generation}')"
  kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" secret "$server_certificate_secret" '{.data.ca\.crt}' | base64 -d > "$ca_file"
  kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" secret "$server_certificate_secret" '{.data.tls\.crt}' | base64 -d > "$cert_file"
  kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" secret "$server_certificate_secret" '{.data.tls\.key}' | base64 -d > "$key_file"
  after="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" secret "$server_certificate_secret" '{.metadata.resourceVersion}')"
  [ "$before" = "$after" ] || mattercodex_die "cert-manager candidate изменилась во время snapshot"
  openssl verify -CAfile "$ca_file" "$cert_file" >/dev/null
  cmp -s <(openssl x509 -in "$ca_file" -outform DER) \
    <(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" secret "$ca_secret" '{.data.tls\.crt}' | base64 -d | openssl x509 -outform DER) ||
    mattercodex_die "candidate leaf не подписан exact CA g1"
  [ "$(openssl x509 -in "$cert_file" -noout -ext subjectAltName | tail -n +2 | tr -d '[:space:]')" = "DNS:${MATTERCODEX_POSTGRES_MIGRATION_TLS_SERVER_NAME}" ] ||
    mattercodex_die "candidate leaf SAN не равен exact Service FQDN"
  [ "$(openssl x509 -in "$cert_file" -pubkey -noout | openssl pkey -pubin -outform DER 2>/dev/null | sha256sum | awk '{print $1}')" = \
    "$(openssl pkey -in "$key_file" -pubout -outform DER 2>/dev/null | sha256sum | awk '{print $1}')" ] ||
    mattercodex_die "candidate leaf и private key не совпадают"
  fingerprint="$(openssl x509 -in "$cert_file" -outform DER | sha256sum | awk '{print $1}')"
  short="${fingerprint:0:16}"
  runtime_secret="mattermost-postgres-migration-runtime-${short}"
  trust_map="mattermost-postgres-migration-trust-${short}"
  content_digest="$({
    sha256sum "$ca_file" | awk '{print $1}'
    sha256sum "$cert_file" | awk '{print $1}'
    sha256sum "$key_file" | awk '{print $1}'
  } | sha256sum | awk '{print $1}')"
  if ! kubectl get secret "$runtime_secret" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" >/dev/null 2>&1; then
    kubectl create secret generic "$runtime_secret" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
      --from-file=ca.crt="$ca_file" --from-file=tls.crt="$cert_file" --from-file=tls.key="$key_file" \
      --dry-run=client -o yaml | kubectl create -f - >/dev/null
    kubectl annotate secret "$runtime_secret" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
      "mattercodex.dev/certificate-generation=${cert_generation}" \
      "mattercodex.dev/source-resource-version=${before}" \
      "mattercodex.dev/server-certificate-sha256=${fingerprint}" \
      "mattercodex.dev/content-sha256=${content_digest}" >/dev/null
    kubectl patch secret "$runtime_secret" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
      --type=merge --patch '{"immutable":true}' >/dev/null
  fi
  [ "$(kubectl get secret "$runtime_secret" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" -o 'go-template={{index .metadata.annotations "mattercodex.dev/server-certificate-sha256"}}')" = "$fingerprint" ] ||
    mattercodex_die "immutable runtime snapshot не совпадает с candidate leaf"
  [ "$(kubectl get secret "$runtime_secret" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" -o 'go-template={{index .metadata.annotations "mattercodex.dev/content-sha256"}}')" = "$content_digest" ] ||
    mattercodex_die "immutable runtime snapshot content не совпадает с candidate leaf/key/CA"
  if ! kubectl get configmap "$trust_map" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" >/dev/null 2>&1; then
    kubectl create configmap "$trust_map" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
      --from-file=ca.pem="$ca_file" --from-file=server.pem="$cert_file" --dry-run=client -o yaml | kubectl create -f - >/dev/null
    kubectl annotate configmap "$trust_map" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
      "mattercodex.dev/server-certificate-sha256=${fingerprint}" >/dev/null
    kubectl patch configmap "$trust_map" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
      --type=merge --patch '{"immutable":true}' >/dev/null
  fi
  cleanup_private_temporary_dir
  printf '%s|%s|%s|%s|%s' "$runtime_secret" "$trust_map" "$fingerprint" "$cert_generation" "$before"
}

secret_value() {
  kubectl get secret "$credential_secret" --namespace "$1" -o "go-template={{index .data \"$2\"}}" | base64 -d
}

principal_comment() {
  admin_query "SELECT coalesce(pg_catalog.shobj_description(oid, 'pg_authid'), '') FROM pg_catalog.pg_roles WHERE rolname = '${principal_name}';" 2>/dev/null
}

principal_exists() {
  [ "$(admin_query "SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = '${principal_name}');" 2>/dev/null)" = t ]
}

lifecycle_comment() {
  local state="$1" attempt="$2" uid rv
  if kubectl get secret "$credential_secret" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" >/dev/null 2>&1; then
    uid="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" secret "$credential_secret" '{.metadata.uid}')"
    rv="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" secret "$credential_secret" '{.metadata.resourceVersion}')"
  else
    uid=missing
    rv=missing
  fi
  printf 'mattercodex:credential-generation=g1;state=%s;secret-uid=%s;secret-rv=%s;rollout-attempt=%s' "$state" "$uid" "$rv" "$attempt"
}

run_principal_sql() {
  local sql_file="$1" comment="$2"
  { printf "\\set lifecycle_comment '%s'\n" "$comment"; sed '/^-- name:/d' "$sql_file"; } |
    kubectl exec -i --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" "${statefulset_name}-0" --container postgres -- \
      sh -ceu 'exec psql -X -q -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"' >/dev/null
}

ensure_source_credential() {
  local secret_exists=false role_exists=false password source_dsn temporary_dir comment current_attempt
  kubectl get secret "$credential_secret" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" >/dev/null 2>&1 && secret_exists=true
  principal_exists && role_exists=true
  [ "$role_exists" = "$secret_exists" ] || mattercodex_die "principal и immutable credential Secret имеют рассогласованный lifecycle"
  if [ "$secret_exists" = true ]; then
    [ "$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" secret "$credential_secret" '{.immutable}')" = true ] ||
      mattercodex_die "credential Secret generation g1 должна быть immutable"
    [ "$(secret_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" username)" = "$principal_name" ] || mattercodex_die "credential Secret содержит другой principal"
    [ "$(secret_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" database)" = "$MATTERCODEX_POSTGRES_DB" ] || mattercodex_die "credential Secret содержит другую database"
    current_attempt="$(index_value current-attempt)"
    [ -n "$current_attempt" ] && [ "$(principal_comment)" = "$(lifecycle_comment CURRENT "$current_attempt")" ] ||
      mattercodex_die "существующий credential generation не exact CURRENT; resurrect g1 запрещён"
    return
  fi
  password="$(openssl rand -hex 32)"
  source_dsn="postgres://${principal_name}:${password}@${MATTERCODEX_POSTGRES_MIGRATION_TLS_SERVER_NAME}:5432/${MATTERCODEX_POSTGRES_DB}?sslmode=verify-full&connect_timeout=10"
  umask 077
  temporary_dir="$(mktemp -d /tmp/mattercodex-postgresql-credential.XXXXXX)"
  private_temporary_dir="$temporary_dir"
  printf '%s' "$principal_name" > "${temporary_dir}/username"
  printf '%s' "$password" > "${temporary_dir}/password"
  printf '%s' "$MATTERCODEX_POSTGRES_DB" > "${temporary_dir}/database"
  printf '%s' "$source_dsn" > "${temporary_dir}/source-dsn"
  kubectl create secret generic "$credential_secret" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
    --from-file=username="${temporary_dir}/username" --from-file=password="${temporary_dir}/password" \
    --from-file=database="${temporary_dir}/database" --from-file=source-dsn="${temporary_dir}/source-dsn" \
    --dry-run=client -o yaml | kubectl create -f - >/dev/null
  kubectl patch secret "$credential_secret" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
    --type=merge --patch '{"immutable":true}' >/dev/null
  comment="$(lifecycle_comment PENDING "$MATTERCODEX_POSTGRES_ROLLOUT_ATTEMPT")"
  { printf "\\set migration_password '%s'\n\\set lifecycle_comment '%s'\n" "$password" "$comment"; sed '/^-- name:/d' "${script_dir}/sql/bootstrap-principal.sql"; } |
    kubectl exec -i --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" "${statefulset_name}-0" --container postgres -- \
      sh -ceu 'exec psql -X -q -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"' >/dev/null
  cleanup_private_temporary_dir
  unset password source_dsn
}

enable_principal_pending() {
  run_principal_sql "${script_dir}/sql/principal-login.sql" "$(lifecycle_comment PENDING "$1")"
}

promote_principal_current() {
  run_principal_sql "${script_dir}/sql/principal-current.sql" "$(lifecycle_comment CURRENT "$1")"
}

retire_principal() {
  local attempt="${1:-00000000000000000000}"
  principal_exists || return 0
  run_principal_sql "${script_dir}/sql/principal-no-login.sql" "$(lifecycle_comment RETIRED "$attempt")"
  sed '/^-- name:/d' "${script_dir}/sql/principal-terminate-sessions.sql" |
    kubectl exec -i --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" "${statefulset_name}-0" --container postgres -- \
      sh -ceu 'exec psql -X -q -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"' >/dev/null
  [ "$(admin_query "SELECT NOT EXISTS (SELECT 1 FROM pg_catalog.pg_stat_activity WHERE usename = '${principal_name}' AND pid <> pg_catalog.pg_backend_pid());")" = t ] ||
    mattercodex_die "zero live migration sessions не доказано"
}

drop_unaccepted_initial_credential() {
  local record="$1" predecessor_attempt
  predecessor_attempt="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" configmap "$record" '{.data.predecessor-attempt}')"
  [ -z "$predecessor_attempt" ] || return 0
  kubectl get secret "$credential_secret" --namespace "$MATTERCODEX_POSTGRES_CLIENT_NAMESPACE" >/dev/null 2>&1 &&
    mattercodex_die "непринятый credential уже опубликован в client namespace; автоматическое удаление запрещено"
  sed '/^-- name:/d' "${script_dir}/sql/principal-drop-unaccepted.sql" |
    kubectl exec -i --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" "${statefulset_name}-0" --container postgres -- \
      sh -ceu 'exec psql -X -q -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"' >/dev/null
  kubectl delete secret "$credential_secret" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" --ignore-not-found >/dev/null
}

create_activation_configmap() {
  local name="$1" state="$2"
  kubectl create configmap "$name" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
    --from-literal=state="$state" --dry-run=client -o yaml | kubectl create -f - >/dev/null
  kubectl patch configmap "$name" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
    --type=merge --patch '{"immutable":true}' >/dev/null
}

local_patched_template() {
  local patch_file="$1" target_file="$2" live_file
  live_file="${private_temporary_dir}/statefulset-live.json"
  kubectl get statefulset "$statefulset_name" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" -o json > "$live_file"
  kubectl patch --local -f "$live_file" --type=strategic --patch-file "$patch_file" -o json | jq -cS '.spec.template' > "$target_file"
}

record_pending_attempt() {
  local attempt="$1" runtime_secret="$2" trust_map="$3" fingerprint="$4" cert_generation="$5" cert_rv="$6"
  local pending_patch="$7" current_patch="$8" record="mattermost-postgres-migration-attempt-${attempt}"
  local predecessor_template pending_template current_template predecessor_digest pending_digest current_digest uid current_revision predecessor_attempt
  umask 077
  private_temporary_dir="$(mktemp -d /tmp/mattercodex-postgresql-ledger.XXXXXX)"
  predecessor_template="${private_temporary_dir}/predecessor-template.json"
  pending_template="${private_temporary_dir}/pending-template.json"
  current_template="${private_temporary_dir}/current-template.json"
  kubectl get statefulset "$statefulset_name" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" -o json | jq -cS '.spec.template' > "$predecessor_template"
  local_patched_template "$pending_patch" "$pending_template"
  local_patched_template "$current_patch" "$current_template"
  predecessor_digest="$(sha256sum "$predecessor_template" | awk '{print $1}')"
  pending_digest="$(sha256sum "$pending_template" | awk '{print $1}')"
  current_digest="$(sha256sum "$current_template" | awk '{print $1}')"
  uid="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" statefulset "$statefulset_name" '{.metadata.uid}')"
  current_revision="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" statefulset "$statefulset_name" '{.status.currentRevision}')"
  predecessor_attempt="$(index_value current-attempt)"
  kubectl create configmap "$record" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
    --from-file=predecessor-template.json="$predecessor_template" \
    --from-literal=attempt="$attempt" --from-literal=state=PENDING --from-literal=source-git-revision="$revision" \
    --from-literal=maintenance-window-id="$maintenance_window_id" --from-literal=statefulset-uid="$uid" \
    --from-literal=predecessor-current-revision="$current_revision" --from-literal=predecessor-attempt="$predecessor_attempt" \
    --from-literal=predecessor-certificate-fingerprint="$(index_value current-certificate-fingerprint)" \
    --from-literal=predecessor-template-digest="$predecessor_digest" \
    --from-literal=pending-template-digest="$pending_digest" --from-literal=current-template-digest="$current_digest" \
    --from-literal=runtime-secret="$runtime_secret" --from-literal=trust-configmap="$trust_map" \
    --from-literal=certificate-generation="$cert_generation" --from-literal=certificate-secret-resource-version="$cert_rv" \
    --from-literal=certificate-fingerprint="$fingerprint" --from-literal=ca-fingerprint="$(ca_fingerprint)" \
    --dry-run=client -o yaml | kubectl create -f - >/dev/null
  kubectl patch configmap "$record" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
    --type=merge --patch '{"immutable":true}' >/dev/null
  cleanup_private_temporary_dir
}

apply_readback_configmap() {
  render_readback_configmap "$1" - | kubectl apply --server-side --field-manager=mattercodex-legacy-postgresql-source -f - >/dev/null
}

run_readback() {
  local namespace="$1" trust_map="$2" job_name expected_fingerprint tls_policy
  expected_fingerprint="$(kubectl get configmap "$trust_map" --namespace "$namespace" -o 'go-template={{index .metadata.annotations "mattercodex.dev/server-certificate-sha256"}}')"
  [[ "$expected_fingerprint" =~ ^[a-f0-9]{64}$ ]] || mattercodex_die "trust snapshot не содержит certificate fingerprint"
  export MATTERCODEX_POSTGRES_READBACK_NAMESPACE="$namespace"
  export MATTERCODEX_POSTGRES_READBACK_TRUST_CONFIGMAP="$trust_map"
  render_template "${template_dir}/readback-job.yaml.tpl" "${render_dir}/readback-job.yaml"
  job_name="$(kubectl create -f "${render_dir}/readback-job.yaml" -o name)"
  if ! kubectl wait --namespace "$namespace" --for=condition=complete --timeout=150s "$job_name" >/dev/null; then
    kubectl logs --namespace "$namespace" "$job_name" --container readback --tail=40 >&2 || true
    return 1
  fi
  kubectl logs --namespace "$namespace" "$job_name" --container readback --tail=1 |
    grep -qx 'legacy PostgreSQL source readback: ok' || return 1
  tls_policy="$(admin_query <<'SQL' 2>/dev/null
SELECT current_setting('ssl') = 'on'
       AND current_setting('ssl_min_protocol_version') = 'TLSv1.3'
       AND current_setting('ssl_max_protocol_version') = 'TLSv1.3';
SQL
)"
  [ "$tls_policy" = t ] || mattercodex_die "PostgreSQL TLS policy readback отклонён"
}

wait_pending_pod() {
  local attempt="$1" deadline observed phase
  deadline=$((SECONDS + max_outage_seconds))
  while [ "$SECONDS" -lt "$deadline" ]; do
    observed="$(kubectl get pod "${statefulset_name}-0" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
      -o 'go-template={{index .metadata.annotations "mattercodex.dev/legacy-postgresql-rollout-attempt"}}' 2>/dev/null || true)"
    phase="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" pod "${statefulset_name}-0" '{.status.phase}' 2>/dev/null || true)"
    if [ "$observed" = "$attempt" ] && [ "$phase" = Running ] &&
       kubectl exec --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" "${statefulset_name}-0" --container postgres -- \
         sh -ceu 'exec pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB"' >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done
  return 1
}

record_acceptance() {
  local attempt="$1" record snapshot digest uid current_revision update_revision fingerprint
  record="mattermost-postgres-migration-accepted-${attempt}"
  umask 077
  private_temporary_dir="$(mktemp -d /tmp/mattercodex-postgresql-acceptance.XXXXXX)"
  snapshot="${private_temporary_dir}/accepted-template.json"
  kubectl get statefulset "$statefulset_name" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" -o json | jq -cS '.spec.template' > "$snapshot"
  digest="$(sha256sum "$snapshot" | awk '{print $1}')"
  uid="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" statefulset "$statefulset_name" '{.metadata.uid}')"
  current_revision="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" statefulset "$statefulset_name" '{.status.currentRevision}')"
  update_revision="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" statefulset "$statefulset_name" '{.status.updateRevision}')"
  [ -n "$current_revision" ] && [ "$current_revision" = "$update_revision" ] || mattercodex_die "applied StatefulSet revision не доказана"
  fingerprint="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" configmap "mattermost-postgres-migration-attempt-${attempt}" '{.data.certificate-fingerprint}')"
  [ "$digest" = "$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" configmap "mattermost-postgres-migration-attempt-${attempt}" '{.data.current-template-digest}')" ] ||
    mattercodex_die "accepted PodTemplate digest не совпадает с pending ledger"
  kubectl create configmap "$record" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
    --from-file=accepted-template.json="$snapshot" --from-literal=attempt="$attempt" --from-literal=state=CURRENT \
    --from-literal=source-git-revision="$revision" --from-literal=statefulset-uid="$uid" \
    --from-literal=applied-revision="$current_revision" --from-literal=pod-template-digest="$digest" \
    --from-literal=served-certificate-fingerprint="$fingerprint" --from-literal=served-state-readback=ok \
    --dry-run=client -o yaml | kubectl create -f - >/dev/null
  kubectl patch configmap "$record" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
    --type=merge --patch '{"immutable":true}' >/dev/null
  cleanup_private_temporary_dir
}

mark_attempt_current() {
  local attempt="$1" record patch rv
  record="mattermost-postgres-migration-accepted-${attempt}"
  rv="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" configmap "$rollout_index" '{.metadata.resourceVersion}')"
  patch="$(jq -cn --arg rv "$rv" --arg attempt "$attempt" \
    --arg uid "$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" configmap "$record" '{.data.statefulset-uid}')" \
    --arg digest "$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" configmap "$record" '{.data.pod-template-digest}')" \
    --arg fingerprint "$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" configmap "$record" '{.data.served-certificate-fingerprint}')" \
    '{metadata:{resourceVersion:$rv},data:{"pending-attempt":"","current-attempt":$attempt,"current-statefulset-uid":$uid,"current-template-digest":$digest,"current-certificate-fingerprint":$fingerprint}}')"
  kubectl patch configmap "$rollout_index" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" --type=merge --patch "$patch" >/dev/null
}

restore_template_from_record() {
  local record="$1" snapshot patch_file
  umask 077
  private_temporary_dir="$(mktemp -d /tmp/mattercodex-postgresql-rollback.XXXXXX)"
  snapshot="${private_temporary_dir}/predecessor-template.json"
  patch_file="${private_temporary_dir}/restore.json"
  kubectl get configmap "$record" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
    -o 'go-template={{index .data "predecessor-template.json"}}' > "$snapshot"
  jq -cn --slurpfile template "$snapshot" '[{"op":"replace","path":"/spec/template","value":$template[0]}]' > "$patch_file"
  kubectl patch statefulset "$statefulset_name" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
    --type=json --patch-file "$patch_file" >/dev/null
  # StatefulSet не удаляет уже сломанный pod после возврата PodTemplate к
  # предыдущей revision, поэтому rollback обязан материализовать predecessor.
  kubectl delete pod "${statefulset_name}-0" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
    --wait=true --timeout="${max_outage_seconds}s" >/dev/null
  kubectl rollout status statefulset "$statefulset_name" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
    --timeout="${max_outage_seconds}s" >/dev/null
  cleanup_private_temporary_dir
}

rollback_attempt() {
  local attempt="$1" record uid expected_uid observed_digest allowed=false predecessor_digest
  local candidate_fingerprint predecessor_fingerprint expected_fingerprint
  record="mattermost-postgres-migration-attempt-${attempt}"
  kubectl get configmap "$record" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" >/dev/null ||
    mattercodex_die "immutable rollback ledger не найден"
  expected_uid="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" configmap "$record" '{.data.statefulset-uid}')"
  uid="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" statefulset "$statefulset_name" '{.metadata.uid}')"
  [ "$uid" = "$expected_uid" ] || mattercodex_die "StatefulSet UID изменился; stale rollback отклонён"
  observed_digest="$(template_digest)"
  predecessor_digest="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" configmap "$record" '{.data.predecessor-template-digest}')"
  for field in predecessor-template-digest pending-template-digest current-template-digest; do
    [ "$observed_digest" = "$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" configmap "$record" "{.data.${field}}")" ] && allowed=true
  done
  [ "$allowed" = true ] || mattercodex_die "current PodTemplate не совпадает с ledger; stale/later rollout rollback отклонён"
  candidate_fingerprint="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" configmap "$record" '{.data.certificate-fingerprint}')"
  predecessor_fingerprint="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" configmap "$record" '{.data.predecessor-certificate-fingerprint}')"
  expected_fingerprint="$candidate_fingerprint"
  [ "$observed_digest" != "$predecessor_digest" ] || expected_fingerprint="$predecessor_fingerprint"
  if [ -n "$expected_fingerprint" ]; then
    [ "$(served_fingerprint)" = "$expected_fingerprint" ] ||
      mattercodex_die "served certificate fingerprint не совпадает с rollback ledger"
  fi
  kubectl delete networkpolicy "$client_ingress" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" --ignore-not-found >/dev/null
  retire_principal "$attempt"
  if [ "$observed_digest" != "$predecessor_digest" ]; then restore_template_from_record "$record"; fi
  [ "$(template_digest)" = "$predecessor_digest" ] || mattercodex_die "exact predecessor PodTemplate не восстановлен"
  functional_checks
  drop_unaccepted_initial_credential "$record"
  local rv patch
  rv="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" configmap "$rollout_index" '{.metadata.resourceVersion}')"
  patch="$(jq -cn --arg rv "$rv" '{metadata:{resourceVersion:$rv},data:{"pending-attempt":"","current-attempt":"","current-statefulset-uid":"","current-template-digest":"","current-certificate-fingerprint":""}}')"
  kubectl patch configmap "$rollout_index" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" --type=merge --patch "$patch" >/dev/null
  mattercodex_log "attempt ${attempt} откатана к exact immutable predecessor; principal RETIRED"
}

reconcile_pending() {
  local pending acceptance uid digest fingerprint applied_revision
  pending="$(index_value pending-attempt)"
  [ -n "$pending" ] || return 0
  acceptance="mattermost-postgres-migration-accepted-${pending}"
  if kubectl get configmap "$acceptance" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" >/dev/null 2>&1; then
    uid="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" statefulset "$statefulset_name" '{.metadata.uid}')"
    digest="$(template_digest)"
    fingerprint="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" configmap "$acceptance" '{.data.served-certificate-fingerprint}')"
    applied_revision="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" configmap "$acceptance" '{.data.applied-revision}')"
    if [ "$uid" = "$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" configmap "$acceptance" '{.data.statefulset-uid}')" ] &&
       [ "$digest" = "$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" configmap "$acceptance" '{.data.pod-template-digest}')" ] &&
       [ "$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" statefulset "$statefulset_name" '{.status.currentRevision}')" = "$applied_revision" ] &&
       [ "$(served_fingerprint)" = "$fingerprint" ] &&
       [ "$(principal_comment)" = "$(lifecycle_comment CURRENT "$pending")" ]; then
      mark_attempt_current "$pending"
      mattercodex_log "crash recovery завершила exact accepted attempt ${pending}"
      return
    fi
  fi
  rollback_attempt "$pending"
}

rollout_source() {
  require_owner_gate
  require_revision
  preflight
  functional_checks
  ensure_rollout_index
  reconcile_pending
  ensure_ca_generation
  render_all
  kubectl apply -f "${render_dir}/pki.yaml" >/dev/null
  kubectl wait --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" --for=condition=Ready --timeout=180s \
    "certificate/${server_certificate}" >/dev/null

  local leaf runtime_secret trust_map fingerprint cert_generation cert_rv attempt pending_activation current_activation pending_patch current_patch
  leaf="$(snapshot_candidate_leaf)"
  IFS='|' read -r runtime_secret trust_map fingerprint cert_generation cert_rv <<< "$leaf"
  attempt="$(allocate_attempt)"
  export MATTERCODEX_POSTGRES_ROLLOUT_ATTEMPT="$attempt"
  export MATTERCODEX_POSTGRES_RUNTIME_SECRET="$runtime_secret"
  export MATTERCODEX_POSTGRES_READBACK_TRUST_CONFIGMAP="$trust_map"
  export MATTERCODEX_POSTGRES_MIGRATION_CERTIFICATE_REVISION="$fingerprint"
  pending_activation="mc-pg-activation-${attempt}-p"
  current_activation="mc-pg-activation-${attempt}-c"
  create_activation_configmap "$pending_activation" PENDING
  create_activation_configmap "$current_activation" CURRENT
  export MATTERCODEX_POSTGRES_ACTIVATION_CONFIGMAP="$pending_activation"
  render_all
  pending_patch="${render_dir}/statefulset-patch-pending.yaml"
  current_patch="${render_dir}/statefulset-patch-current.yaml"
  cp "${render_dir}/statefulset-patch.yaml" "$pending_patch"
  export MATTERCODEX_POSTGRES_ACTIVATION_CONFIGMAP="$current_activation"
  render_template "${template_dir}/statefulset-patch.yaml.tpl" "$current_patch"
  record_pending_attempt "$attempt" "$runtime_secret" "$trust_map" "$fingerprint" "$cert_generation" "$cert_rv" "$pending_patch" "$current_patch"
  mark_attempt_pending "$attempt"

  if ! (
    kubectl apply -f "${render_dir}/runtime.yaml" >/dev/null &&
      apply_readback_configmap "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" &&
      ensure_source_credential &&
      kubectl delete networkpolicy "$client_ingress" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" --ignore-not-found >/dev/null &&
      enable_principal_pending "$attempt" &&
      kubectl patch statefulset "$statefulset_name" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
        --type=strategic --patch-file "$pending_patch" >/dev/null &&
      wait_pending_pod "$attempt" &&
      run_readback "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" "$trust_map" &&
      kubectl patch statefulset "$statefulset_name" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
        --type=strategic --patch-file "$current_patch" >/dev/null &&
      kubectl rollout status statefulset "$statefulset_name" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" \
        --timeout="${max_outage_seconds}s" >/dev/null &&
      run_readback "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" "$trust_map" &&
      functional_checks &&
      record_acceptance "$attempt" &&
      promote_principal_current "$attempt" &&
      mark_attempt_current "$attempt"
  ); then
    rollback_attempt "$attempt"
    mattercodex_die "rollout attempt ${attempt} не принята и откатана"
  fi
  mattercodex_log "attempt ${attempt} CURRENT; client ingress остаётся закрыт до publish-client"
}

copy_credential_to_client_namespace() {
  local source_digest target_digest
  if kubectl get secret "$credential_secret" --namespace "$MATTERCODEX_POSTGRES_CLIENT_NAMESPACE" >/dev/null 2>&1; then
    source_digest="$(kubectl get secret "$credential_secret" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" -o json | jq -cS '.data' | sha256sum | awk '{print $1}')"
    target_digest="$(kubectl get secret "$credential_secret" --namespace "$MATTERCODEX_POSTGRES_CLIENT_NAMESPACE" -o json | jq -cS '.data' | sha256sum | awk '{print $1}')"
    [ "$source_digest" = "$target_digest" ] || mattercodex_die "client credential Secret имеет другое immutable content"
    return
  fi
  kubectl get secret "$credential_secret" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" -o json |
    jq --arg namespace "$MATTERCODEX_POSTGRES_CLIENT_NAMESPACE" '
      del(.metadata.annotations,.metadata.creationTimestamp,.metadata.managedFields,.metadata.ownerReferences,.metadata.resourceVersion,.metadata.uid)
      | .metadata.namespace=$namespace' | kubectl create -f - >/dev/null
}

copy_trust_to_client_namespace() {
  local source_map="$1"
  kubectl get configmap "$source_map" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" -o json |
    jq --arg namespace "$MATTERCODEX_POSTGRES_CLIENT_NAMESPACE" '
      del(.metadata.creationTimestamp,.metadata.managedFields,.metadata.ownerReferences,.metadata.resourceVersion,.metadata.uid)
      | .metadata.namespace=$namespace | .metadata.name="mattermost-postgresql-ca" | .immutable=false' |
    kubectl apply --server-side --field-manager=mattercodex-legacy-postgresql-source -f - >/dev/null
}

assert_current_acceptance() {
  local attempt record uid digest fingerprint applied_revision
  attempt="$(index_value current-attempt)"
  [ -n "$attempt" ] || mattercodex_die "CURRENT rollout отсутствует"
  record="mattermost-postgres-migration-accepted-${attempt}"
  kubectl get configmap "$record" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" >/dev/null || mattercodex_die "acceptance record отсутствует"
  uid="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" statefulset "$statefulset_name" '{.metadata.uid}')"
  digest="$(template_digest)"
  fingerprint="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" configmap "$record" '{.data.served-certificate-fingerprint}')"
  applied_revision="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" configmap "$record" '{.data.applied-revision}')"
  [ "$uid" = "$(index_value current-statefulset-uid)" ] && [ "$digest" = "$(index_value current-template-digest)" ] &&
    [ "$fingerprint" = "$(index_value current-certificate-fingerprint)" ] ||
    mattercodex_die "current served state не совпадает с durable acceptance ledger"
  [ "$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" statefulset "$statefulset_name" '{.status.currentRevision}')" = "$applied_revision" ] &&
    [ "$(served_fingerprint)" = "$fingerprint" ] ||
    mattercodex_die "current applied revision/served fingerprint не совпадает с acceptance record"
  [ "$(principal_comment)" = "$(lifecycle_comment CURRENT "$attempt")" ] ||
    mattercodex_die "migration principal не CURRENT для exact attempt"
  printf '%s' "$attempt"
}

publish_client() {
  require_owner_gate
  require_revision
  preflight
  ensure_rollout_index
  reconcile_pending
  local attempt record trust_map
  attempt="$(assert_current_acceptance)"
  record="mattermost-postgres-migration-attempt-${attempt}"
  trust_map="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" configmap "$record" '{.data.trust-configmap}')"
  kubectl get namespace "$MATTERCODEX_POSTGRES_CLIENT_NAMESPACE" >/dev/null || mattercodex_die "client namespace ещё не создан"
  export MATTERCODEX_POSTGRES_READBACK_NAMESPACE="$MATTERCODEX_POSTGRES_CLIENT_NAMESPACE"
  export MATTERCODEX_POSTGRES_READBACK_TRUST_CONFIGMAP=mattermost-postgresql-ca
  export MATTERCODEX_POSTGRES_ROLLOUT_ATTEMPT="$attempt"
  export MATTERCODEX_POSTGRES_MIGRATION_CERTIFICATE_REVISION="$(index_value current-certificate-fingerprint)"
  render_all
  kubectl apply -f "${render_dir}/client-runtime.yaml" >/dev/null
  apply_readback_configmap "$MATTERCODEX_POSTGRES_CLIENT_NAMESPACE"
  copy_credential_to_client_namespace
  copy_trust_to_client_namespace "$trust_map"
  kubectl apply -f "${render_dir}/client-ingress.yaml" >/dev/null
  if ! run_readback "$MATTERCODEX_POSTGRES_CLIENT_NAMESPACE" mattermost-postgresql-ca; then
    kubectl delete networkpolicy "$client_ingress" --namespace "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" --ignore-not-found >/dev/null
    kubectl delete secret "$credential_secret" --namespace "$MATTERCODEX_POSTGRES_CLIENT_NAMESPACE" --ignore-not-found >/dev/null
    mattercodex_die "client readback отклонён; ingress и client credential удалены"
  fi
  mattercodex_log "client path опубликован только для exact CURRENT attempt ${attempt}"
}

readback_current() {
  require_owner_gate
  require_revision
  ensure_rollout_index
  local attempt record trust_map namespace
  attempt="$(assert_current_acceptance)"
  record="mattermost-postgres-migration-attempt-${attempt}"
  if [ "$readback_scope" = source ]; then
    namespace="$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE"
    trust_map="$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" configmap "$record" '{.data.trust-configmap}')"
  else
    namespace="$MATTERCODEX_POSTGRES_CLIENT_NAMESPACE"
    trust_map=mattermost-postgresql-ca
  fi
  export MATTERCODEX_POSTGRES_READBACK_NAMESPACE="$namespace"
  export MATTERCODEX_POSTGRES_READBACK_TRUST_CONFIGMAP="$trust_map"
  export MATTERCODEX_POSTGRES_ROLLOUT_ATTEMPT="$attempt"
  render_all
  apply_readback_configmap "$namespace"
  run_readback "$namespace" "$trust_map" || mattercodex_die "served-state readback отклонён"
}

manual_rollback() {
  require_owner_gate
  require_revision
  [ -n "$requested_attempt" ] || mattercodex_die "rollback требует exact --attempt"
  ensure_rollout_index
  [ "$(assert_current_acceptance)" = "$requested_attempt" ] || mattercodex_die "rollback разрешён только для exact current accepted attempt"
  [ "$(kubectl_value "$MATTERCODEX_LEGACY_POSTGRES_NAMESPACE" configmap "mattermost-postgres-migration-attempt-${requested_attempt}" '{.data.source-git-revision}')" = "$revision" ] ||
    mattercodex_die "rollback Git SHA не совпадает с immutable ledger"
  functional_checks
  rollback_attempt "$requested_attempt"
}

validate_inputs

case "$command_name" in
  render) render_all ;;
  preflight) preflight ;;
  apply|renew) rollout_source ;;
  publish-client) publish_client ;;
  readback) readback_current ;;
  rollback) manual_rollback ;;
  *) mattercodex_die "неизвестная команда: $command_name" ;;
esac
