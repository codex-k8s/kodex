#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex Kubernetes Secret materialization failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 --context <exact-context> --material-directory <path>" \
    '  --oidc-ca-file <path> --provider-auth-file <path>' >&2
}

expected_context=""
material_directory=""
oidc_ca_file=""
provider_auth_file=""
while (($# > 0)); do
  case "$1" in
    --context) expected_context="${2:-}"; shift 2 ;;
    --material-directory) material_directory="${2:-}"; shift 2 ;;
    --oidc-ca-file) oidc_ca_file="${2:-}"; shift 2 ;;
    --provider-auth-file) provider_auth_file="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$expected_context" ]] || fail 'exact Kubernetes context is required'
[[ -d "$material_directory" && ! -L "$material_directory" ]] ||
  fail 'material directory is invalid'
for file_path in "$oidc_ca_file" "$provider_auth_file"; do
  [[ -r "$file_path" && -s "$file_path" && ! -L "$file_path" ]] ||
    fail 'required input material is invalid'
done
for command_name in jq kubectl openssl sha256sum stat; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ "$(kubectl config current-context)" == "$expected_context" ]] ||
  fail 'current Kubernetes context mismatch'
openssl x509 -in "$oidc_ca_file" -noout -checkend 3600 >/dev/null ||
  fail 'OIDC trust certificate is invalid or expires too soon'
jq -e '
  type == "object" and
  ((.auth_mode == "chatgpt" and (.tokens | type == "object")) or
   (.auth_mode == "chatgptAuthTokens" and (.tokens | type == "object")) or
   (.auth_mode == "apikey" and
     (.OPENAI_API_KEY | type == "string" and length > 0)))
' "$provider_auth_file" >/dev/null ||
  fail 'provider authorization JSON is invalid'

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
registry_file="$repository_root/tools/install/secret-projections.json"
jq -e '
  .version == 1 and .namespace == "kodex-system" and (.secrets | length > 0) and
  ([.secrets[].name] | length == (unique | length)) and
  all(.secrets[]; (.items | type == "array" and length > 0) and
    ([.items[].key] | length == (unique | length)) and
    all(.items[]; ((.required // true) | type == "boolean")))
' "$registry_file" >/dev/null || fail 'secret projection registry is invalid'
namespace=$(jq -er '.namespace' "$registry_file")
runtime_namespace=kodex-runtime
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
umask 077

for namespace_name in kodex-system kodex-runtime kodex-trust; do
  kubectl create namespace "$namespace_name" --dry-run=client -o yaml |
    kubectl apply --server-side --field-manager=kodex-install -f - >/dev/null
done

apply_secret_from_directory() {
  local namespace_name=$1 secret_name=$2 directory=$3 manifest key_count
  [[ -d "$directory" && ! -L "$directory" ]] ||
    fail "secret projection directory is absent: $secret_name"
  key_count=$(find "$directory" -mindepth 1 -maxdepth 1 -type f | wc -l)
  ((key_count > 0)) || fail "secret projection is empty: $secret_name"
  manifest="$temporary_directory/$namespace_name-$secret_name.yaml"
  arguments=()
  while IFS= read -r file_path; do
    arguments+=("--from-file=$(basename -- "$file_path")=$file_path")
  done < <(find "$directory" -mindepth 1 -maxdepth 1 -type f | sort)
  kubectl -n "$namespace_name" create secret generic "$secret_name" \
    "${arguments[@]}" --dry-run=client -o yaml >"$manifest"
  kubectl apply --server-side --force-conflicts --field-manager=kodex-install \
    -f "$manifest" >/dev/null
}

while IFS=$'\t' read -r secret_name dynamic; do
  if [[ "$dynamic" == true ]]; then
    if ! kubectl -n "$namespace" get secret "$secret_name" >/dev/null 2>&1; then
      jq -n --arg namespace "$namespace" --arg name "$secret_name" '{
        apiVersion:"v1",kind:"Secret",
        metadata:{namespace:$namespace,name:$name,labels:{
          "app.kubernetes.io/part-of":"kodex",
          "app.kubernetes.io/managed-by":"internal-rpc-authority-publisher"
        },annotations:{"kodex.dev/secret-generation":"0"}},
        type:"Opaque",data:{}
      }' | kubectl create --field-manager=kodex-install -f - >/dev/null
    fi
    kubectl -n "$namespace" get secret "$secret_name" -o json | jq -e '
      .type == "Opaque" and
      (.metadata.annotations["kodex.dev/secret-generation"] | test("^(0|[1-9][0-9]*)$"))
    ' >/dev/null || fail "dynamic authority Secret readback failed: $secret_name"
    continue
  fi
  apply_secret_from_directory "$namespace" "$secret_name" \
    "$material_directory/projections/$secret_name"
done < <(jq -r '.secrets[] | [.name,((.dynamic // false)|tostring)] | @tsv' "$registry_file")

create_secret() {
  local namespace_name=$1 name=$2
  shift 2
  kubectl -n "$namespace_name" create secret generic "$name" "$@" \
    --dry-run=client -o yaml |
    kubectl apply --server-side --force-conflicts --field-manager=kodex-install -f - >/dev/null
}

apply_configmap() {
  local namespace_name=$1 name=$2
  shift 2
  kubectl -n "$namespace_name" create configmap "$name" "$@" --dry-run=client -o yaml |
    kubectl apply --server-side --force-conflicts --field-manager=kodex-install -f - >/dev/null
}

preserve_selected_provider_metadata() {
  local metadata selected_name selected_secret selected_digest
  metadata=$(kubectl -n kodex-system get configmap \
    runtime-provider-openai-default-metadata -o json 2>/dev/null) || return 1
  jq -e '
    .metadata.annotations["kodex.dev/provider-account-key"] == "default-openai-codex" and
    (.data.secretName | test("^runtime-provider-openai-[a-z0-9-]{1,160}$")) and
    (.data.secretUID | type == "string" and length > 0) and
    (.data.secretResourceVersion | type == "string" and length > 0) and
    (.data.contentSHA256 | test("^[a-f0-9]{64}$"))
  ' <<<"$metadata" >/dev/null || return 1
  selected_name=$(jq -er '.data.secretName' <<<"$metadata")
  selected_secret=$(kubectl -n "$runtime_namespace" get "secret/$selected_name" -o json 2>/dev/null) || return 1
  selected_digest=$(jq -jr '.data["auth.json"] // "" | @base64d' <<<"$selected_secret" |
    sha256sum | awk '{print $1}')
  jq -e --arg name "$selected_name" --arg digest "$selected_digest" \
    --arg uid "$(jq -r '.metadata.uid' <<<"$selected_secret")" \
    --arg resource_version "$(jq -r '.metadata.resourceVersion' <<<"$selected_secret")" '
      .immutable == true and .type == "Opaque" and
      .metadata.name == $name and
      .metadata.namespace == "kodex-runtime" and
      .metadata.annotations["kodex.dev/provider-account-key"] == "default-openai-codex" and
      ((.data["auth.sha256"] // "" | @base64d | gsub("[[:space:]]"; "")) == $digest) and
      $uid != "" and $resource_version != ""
    ' <<<"$selected_secret" >/dev/null || return 1
  jq -e --arg uid "$(jq -r '.metadata.uid' <<<"$selected_secret")" \
    --arg resource_version "$(jq -r '.metadata.resourceVersion' <<<"$selected_secret")" \
    --arg digest "$selected_digest" '
      .data.secretUID == $uid and
      .data.secretResourceVersion == $resource_version and
      .data.contentSHA256 == $digest
  ' <<<"$metadata" >/dev/null
}

restore_selected_provider_metadata_from_auth() {
  local digest candidates selected_name selected_uid selected_resource_version
  digest=$(sha256sum "$provider_auth_file" | awk '{print $1}')
  candidates=$(kubectl -n "$runtime_namespace" get secrets -o json | jq -c \
    --arg digest "$digest" '
      [.items[] |
        select(.immutable == true and .type == "Opaque") |
        select(.metadata.annotations["kodex.dev/provider-account-key"] == "default-openai-codex") |
        select(.metadata.name | test("^runtime-provider-openai-[a-z0-9-]{1,160}$")) |
        select((.data["auth.sha256"] // "" | @base64d | gsub("[[:space:]]"; "")) == $digest) |
        {name:.metadata.name,uid:.metadata.uid,resourceVersion:.metadata.resourceVersion}]
    ')
  [[ "$(jq -r 'length' <<<"$candidates")" == 1 ]] || return 1
  selected_name=$(jq -er '.[0].name' <<<"$candidates")
  selected_uid=$(jq -er '.[0].uid | select(type == "string" and length > 0)' <<<"$candidates")
  selected_resource_version=$(jq -er \
    '.[0].resourceVersion | select(type == "string" and length > 0)' <<<"$candidates")
  apply_configmap kodex-system runtime-provider-openai-default-metadata \
    --from-literal=secretName="$selected_name" \
    --from-literal=secretUID="$selected_uid" \
    --from-literal=secretResourceVersion="$selected_resource_version" \
    --from-literal=contentSHA256="$digest"
  kubectl -n kodex-system annotate configmap runtime-provider-openai-default-metadata \
    kodex.dev/provider-account-key=default-openai-codex --overwrite >/dev/null
}

materialize_provider_secret() {
  local name=runtime-provider-openai-default-r1
  local digest digest_file manifest current current_digest current_digest_file
  digest=$(sha256sum "$provider_auth_file" | awk '{print $1}')
  digest_file="$temporary_directory/provider-auth.sha256"
  manifest="$temporary_directory/provider-secret.json"
  printf '%s\n' "$digest" >"$digest_file"

  if current=$(kubectl -n "$runtime_namespace" get "secret/$name" \
    --show-managed-fields -o json 2>/dev/null); then
    current_digest=$(jq -jr '.data["auth.json"] // "" | @base64d' <<<"$current" |
      sha256sum | awk '{print $1}')
    current_digest_file=$(jq -jr '.data["auth.sha256"] // "" | @base64d' \
      <<<"$current" | tr -d '[:space:]')
    if jq -e '.immutable == true' <<<"$current" >/dev/null; then
      [[ "$current_digest" == "$digest" && "$current_digest_file" == "$digest" ]] ||
        fail 'immutable provider credential differs from installation material; create a new credential revision'
      return
    fi
    jq -e --arg namespace "$runtime_namespace" --arg name "$name" '
      .metadata.namespace == $namespace and .metadata.name == $name and
      .type == "Opaque" and ((.metadata.ownerReferences // []) | length == 0) and
      any(.metadata.managedFields[]?; .manager == "kodex-install")
    ' <<<"$current" >/dev/null ||
      fail 'mutable provider credential is not owned by the Kodex installer'
    kubectl -n "$runtime_namespace" delete "secret/$name" --wait=true --timeout=3m >/dev/null
  fi

  kubectl -n "$runtime_namespace" create secret generic "$name" \
    --from-file=auth.json="$provider_auth_file" \
    --from-file=auth.sha256="$digest_file" \
    --dry-run=client -o json | jq '
      .immutable = true |
      .metadata.labels = {
        "app.kubernetes.io/part-of":"kodex",
        "app.kubernetes.io/managed-by":"kodex-install"
      } |
      .metadata.annotations = {
        "kodex.dev/provider-account-key":"default-openai-codex"
      }
    ' >"$manifest"
  kubectl create --field-manager=kodex-install -f "$manifest" >/dev/null
}

installation_ca="$material_directory/authorities/pki"
runtime_execution_certificate="$material_directory/material/kodex/runtime-execution-client/tls/tls.crt"
openssl verify -CAfile "$installation_ca/ca.crt" "$runtime_execution_certificate" >/dev/null ||
  fail 'runtime execution client certificate is not signed by the installation CA'
[[ "$(openssl x509 -in "$runtime_execution_certificate" -noout -ext subjectAltName)" == \
  *"URI:spiffe://kodex.local/ns/kodex-runtime/sa/agent-runner"* ]] ||
  fail 'runtime execution client certificate SPIFFE identity is invalid'
create_secret kodex-system kodex-installation-ca \
  --from-file=tls.crt="$installation_ca/ca.crt" \
  --from-file=tls.key="$installation_ca/ca.key"
create_secret kodex-trust kodex-installation-ca \
  --from-file=tls.crt="$installation_ca/ca.crt"
create_secret "$runtime_namespace" runtime-execution-client-tls \
  --from-file=tls.crt="$runtime_execution_certificate" \
  --from-file=tls.key="$material_directory/material/kodex/runtime-execution-client/tls/tls.key" \
  --from-file=ca.crt="$material_directory/material/kodex/runtime-execution-client/tls/ca.crt"
create_secret kodex-system kodex-postgresql-bootstrap \
  --from-file=password="$material_directory/postgresql/bootstrap-password"

runtime_arguments=()
while IFS= read -r role_file; do
  runtime_arguments+=("--from-file=$(basename -- "$role_file")=$role_file")
done < <(find "$material_directory/postgresql/roles" -mindepth 1 -maxdepth 1 -type f | sort)
create_secret kodex-system kodex-postgresql-runtime-credentials "${runtime_arguments[@]}"

create_secret kodex-system kodex-nats-credentials \
  --from-file=operator.jwt="$material_directory/nats/operator.jwt" \
  --from-file=system-account.public="$material_directory/nats/system-account.public" \
  --from-file=system-account.jwt="$material_directory/nats/system-account.jwt" \
  --from-file=account.public="$material_directory/nats/account.public" \
  --from-file=account.jwt="$material_directory/nats/account.jwt"
create_secret kodex-system kodex-sentry --from-literal=dsn=
create_secret kodex-system internal-rpc-authority-sentry --from-literal=dsn=
create_secret kodex-system kodex-integration-credentials --from-literal=empty=
selected_provider_metadata_preserved=false
if preserve_selected_provider_metadata; then
  selected_provider_metadata_preserved=true
elif restore_selected_provider_metadata_from_auth && preserve_selected_provider_metadata; then
  selected_provider_metadata_preserved=true
else
  materialize_provider_secret
fi
if legacy_provider=$(kubectl -n kodex-system get secret runtime-provider-openai-default-r1 -o json 2>/dev/null); then
  jq -e '
    .metadata.labels["app.kubernetes.io/managed-by"] == "kodex-install" and
    ((.metadata.ownerReferences // []) | length == 0)
  ' <<<"$legacy_provider" >/dev/null ||
    fail 'legacy provider credential in control namespace is not owned by the Kodex installer'
  kubectl -n kodex-system delete secret runtime-provider-openai-default-r1 \
    --wait=true --timeout=3m >/dev/null
fi

apply_configmap kodex-system kodex-oidc-ca --from-file=ca.pem="$oidc_ca_file"
for configmap_name in kodex-internal-ca kodex-otel-ca internal-rpc-authority-otel-ca; do
  apply_configmap kodex-system "$configmap_name" --from-file=ca.pem="$installation_ca/ca.crt"
done

if [[ "$selected_provider_metadata_preserved" != true ]]; then
  provider_uid=$(kubectl -n "$runtime_namespace" get secret runtime-provider-openai-default-r1 \
    -o jsonpath='{.metadata.uid}')
  provider_resource_version=$(kubectl -n "$runtime_namespace" get secret runtime-provider-openai-default-r1 \
    -o jsonpath='{.metadata.resourceVersion}')
  provider_sha256=$(sha256sum "$provider_auth_file" | awk '{print $1}')
  apply_configmap kodex-system runtime-provider-openai-default-metadata \
    --from-literal=secretName=runtime-provider-openai-default-r1 \
    --from-literal=secretUID="$provider_uid" \
    --from-literal=secretResourceVersion="$provider_resource_version" \
    --from-literal=contentSHA256="$provider_sha256"
fi

manifest_root="$material_directory/crypto/authority-bootstrap/public/manifest-root"
readback_root="$material_directory/crypto/authority-bootstrap/public/readback-root"
roots_digest=$(
  {
    for file_path in \
      "$manifest_root/bootstrap-public.jwk" "$manifest_root/bootstrap-metadata.json" \
      "$readback_root/bootstrap-public.jwk" "$readback_root/bootstrap-metadata.json"; do
      printf '%s\n' "${file_path#"$material_directory"/}"
      sha256sum "$file_path" | awk '{print $1}'
    done
  } | sha256sum | awk '{print $1}'
)
if kubectl -n kodex-system get secret internal-rpc-authority-bootstrap-roots >/dev/null 2>&1; then
  [[ "$(kubectl -n kodex-system get secret internal-rpc-authority-bootstrap-roots \
    -o jsonpath='{.metadata.annotations.kodex\.dev/authority-bootstrap-roots-sha256}')" == "$roots_digest" ]] ||
    fail 'immutable authority bootstrap roots differ from generated material'
else
  roots_manifest="$temporary_directory/authority-bootstrap-roots.yaml"
  kubectl -n kodex-system create secret generic internal-rpc-authority-bootstrap-roots \
    --from-file=manifest-root-public.jwk="$manifest_root/bootstrap-public.jwk" \
    --from-file=manifest-root-metadata.json="$manifest_root/bootstrap-metadata.json" \
    --from-file=readback-root-public.jwk="$readback_root/bootstrap-public.jwk" \
    --from-file=readback-root-metadata.json="$readback_root/bootstrap-metadata.json" \
    --dry-run=client -o json | jq --arg digest "$roots_digest" '
      .immutable=true |
      .metadata.labels={"app.kubernetes.io/name":"internal-rpc-authority",
        "app.kubernetes.io/component":"bootstrap-roots"} |
      .metadata.annotations={"kodex.dev/authority-bootstrap-roots-sha256":$digest}
    ' >"$roots_manifest"
  kubectl create --field-manager=kodex-install -f "$roots_manifest" >/dev/null
fi

for secret_name in kodex-installation-ca kodex-postgresql-bootstrap \
  kodex-postgresql-runtime-credentials kodex-nats-credentials kodex-sentry \
  internal-rpc-authority-sentry \
  internal-rpc-authority-bootstrap-roots; do
  kubectl -n kodex-system get secret "$secret_name" -o json | jq -e \
    '.data | type == "object"' >/dev/null || fail "Secret readback failed: $secret_name"
done
secret_name=runtime-execution-client-tls
kubectl -n "$runtime_namespace" get secret "$secret_name" -o json | jq -e \
  '.data | type == "object"' >/dev/null || fail "runtime Secret readback failed: $secret_name"
preserve_selected_provider_metadata || fail 'active provider credential readback failed'
printf 'Kodex Kubernetes Secrets materialized\n'
