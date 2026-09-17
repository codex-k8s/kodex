#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex Kubernetes Secret materialization failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 --context <exact-context> --material-directory <path>" \
    '  --oidc-ca-file <path> [--provider-auth-file <path>]' \
    '  [--security-profile protected|trusted-cluster] [--provider-mode configured|deferred]' \
    '  [--render <path> (required for trusted-cluster)]' >&2
}

expected_context=""
material_directory=""
oidc_ca_file=""
provider_auth_file=""
security_profile=protected
provider_mode=configured
render_file=""
while (($# > 0)); do
  case "$1" in
    --context) expected_context="${2:-}"; shift 2 ;;
    --material-directory) material_directory="${2:-}"; shift 2 ;;
    --oidc-ca-file) oidc_ca_file="${2:-}"; shift 2 ;;
    --provider-auth-file) provider_auth_file="${2:-}"; shift 2 ;;
    --security-profile) security_profile="${2:-}"; shift 2 ;;
    --provider-mode) provider_mode="${2:-}"; shift 2 ;;
    --render) render_file="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$expected_context" ]] || fail 'exact Kubernetes context is required'
case "$security_profile" in protected|trusted-cluster) ;; *) fail 'security profile is invalid' ;; esac
case "$provider_mode" in configured|deferred) ;; *) fail 'provider mode is invalid' ;; esac
if [[ "$provider_mode" == deferred ]]; then
  [[ "$security_profile" == trusted-cluster && -z "$provider_auth_file" ]] ||
    fail 'deferred provider requires explicit trusted-cluster and no authorization file'
fi
repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
if [[ "$security_profile" == trusted-cluster ]]; then
  [[ -f "$render_file" && ! -L "$render_file" ]] || fail 'trusted-cluster render is required'
  python3 - "$render_file" <<'PY' |
import json
import sys
import yaml
with open(sys.argv[1]) as source:
    resources = [resource for resource in yaml.safe_load_all(source) if resource]
if not any(resource.get('kind') == 'Deployment' for resource in resources):
    raise SystemExit('Trusted render contains no deployments')
json.dump(resources, sys.stdout)
PY
    python3 "$repository_root/tools/dev/trusted_cluster_render.py" verify --profile trusted-cluster ||
    fail 'trusted-cluster render verification failed'
elif [[ -n "$render_file" ]]; then
  fail 'render selection requires trusted-cluster'
fi
[[ -d "$material_directory" && ! -L "$material_directory" ]] ||
  fail 'material directory is invalid'
material_directory=$(cd -- "$material_directory" && pwd -P)
input_files=("$oidc_ca_file")
[[ "$provider_mode" == deferred ]] || input_files+=("$provider_auth_file")
for file_path in "${input_files[@]}"; do
  [[ -r "$file_path" && -s "$file_path" && ! -L "$file_path" ]] ||
    fail 'required input material is invalid'
done
for command_name in jq kubectl openssl sha256sum stat python3; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ "$(kubectl config current-context)" == "$expected_context" ]] ||
  fail 'current Kubernetes context mismatch'
openssl x509 -in "$oidc_ca_file" -noout -checkend 3600 >/dev/null ||
  fail 'OIDC trust certificate is invalid or expires too soon'
if [[ "$provider_mode" == configured ]]; then
jq -e '
  type == "object" and
  ((.auth_mode == "chatgpt" and (.tokens | type == "object")) or
   (.auth_mode == "chatgptAuthTokens" and (.tokens | type == "object")) or
   (.auth_mode == "apikey" and
     (.OPENAI_API_KEY | type == "string" and length > 0)))
' "$provider_auth_file" >/dev/null ||
  fail 'provider authorization JSON is invalid'
fi

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

for namespace_name in kodex-system kodex-runtime kodex-trust kodex-secret-drafts; do
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
  check_secret_owner "$namespace_name" "$secret_name"
  arguments=()
  while IFS= read -r file_path; do
    arguments+=("--from-file=$(basename -- "$file_path")=$file_path")
  done < <(find "$directory" -mindepth 1 -maxdepth 1 -type f | sort)
  kubectl -n "$namespace_name" create secret generic "$secret_name" \
    "${arguments[@]}" --dry-run=client -o json | label_secret >"$manifest"
  kubectl apply --server-side --field-manager=kodex-install \
    -f "$manifest" >/dev/null
}

check_secret_owner() {
  local namespace_name=$1 secret_name=$2 existing
  [[ "$security_profile" == trusted-cluster ]] || return 0
  existing=$(kubectl -n "$namespace_name" get secret "$secret_name" --ignore-not-found \
    -o 'jsonpath={.metadata.labels.app\.kubernetes\.io/part-of}{"/"}{.metadata.labels.kodex\.dev/security-profile}')
  [[ -z "$existing" || "$existing" == kodex/trusted-cluster ]] ||
    fail "existing Secret ownership mismatch: $namespace_name/$secret_name"
}

label_secret() {
  jq --arg profile "$security_profile" '
    .metadata.labels["app.kubernetes.io/part-of"]="kodex" |
    .metadata.labels["kodex.dev/security-profile"]=$profile
  '
}

while IFS=$'\t' read -r secret_name dynamic; do
  if [[ "$security_profile" == trusted-cluster && "$secret_name" == internal-rpc-authority-* ]]; then
    continue
  fi
  if [[ "$secret_name" == secret-broker-draft-keyring ]]; then
    # Отдельный владелец rotation/guard: общий apply не перезаписывает ключи.
    bash "$repository_root/tools/install/bootstrap-secret-drafts.sh" ensure \
      --context "$expected_context" \
      --keyring-file "$material_directory/projections/$secret_name/keyring.json"
    continue
  fi
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
  check_secret_owner "$namespace_name" "$name"
  kubectl -n "$namespace_name" create secret generic "$name" "$@" \
    --dry-run=client -o json | label_secret |
    kubectl apply --server-side --field-manager=kodex-install -f - >/dev/null
}

apply_configmap() {
  local namespace_name=$1 name=$2
  shift 2
  kubectl -n "$namespace_name" create configmap "$name" "$@" --dry-run=client -o yaml |
    kubectl apply --server-side --force-conflicts --field-manager=kodex-install -f - >/dev/null
}

installation_ca="$material_directory/authorities/pki"
runtime_execution_certificate="$material_directory/material/kodex/runtime-execution-client/tls/tls.crt"
if [[ "$security_profile" == protected ]]; then
openssl verify -CAfile "$installation_ca/ca.crt" "$runtime_execution_certificate" >/dev/null ||
  fail 'runtime execution client certificate is not signed by the installation CA'
[[ "$(openssl x509 -in "$runtime_execution_certificate" -noout -ext subjectAltName)" == \
  *"URI:spiffe://kodex.local/ns/kodex-runtime/sa/agent-runner"* ]] ||
  fail 'runtime execution client certificate SPIFFE identity is invalid'
fi
create_secret kodex-system kodex-installation-ca \
  --from-file=tls.crt="$installation_ca/ca.crt" \
  --from-file=tls.key="$installation_ca/ca.key"
create_secret kodex-trust kodex-installation-ca \
  --from-file=tls.crt="$installation_ca/ca.crt"
if [[ "$security_profile" == protected ]]; then
create_secret "$runtime_namespace" runtime-execution-client-tls \
  --from-file=tls.crt="$runtime_execution_certificate" \
  --from-file=tls.key="$material_directory/material/kodex/runtime-execution-client/tls/tls.key" \
  --from-file=ca.crt="$material_directory/material/kodex/runtime-execution-client/tls/ca.crt"
fi
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
if [[ "$security_profile" == protected ]]; then
  create_secret kodex-system internal-rpc-authority-sentry --from-literal=dsn=
fi
create_secret kodex-system kodex-integration-credentials --from-literal=empty=
if [[ "$provider_mode" == configured ]]; then
python3 "$repository_root/tools/install/provider-bootstrap.py" seed \
  --context "$expected_context" --auth-file "$provider_auth_file"
fi
# Legacy provider objects сохраняются: bootstrap не выполняет неявную очистку.

apply_configmap kodex-system kodex-oidc-ca --from-file=ca.pem="$oidc_ca_file"
for configmap_name in kodex-internal-ca kodex-otel-ca internal-rpc-authority-otel-ca; do
  if [[ "$security_profile" == trusted-cluster && "$configmap_name" == internal-rpc-authority-* ]]; then
    continue
  fi
  apply_configmap kodex-system "$configmap_name" --from-file=ca.pem="$installation_ca/ca.crt"
done

if [[ "$security_profile" == protected ]]; then
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
fi

for secret_name in kodex-installation-ca kodex-postgresql-bootstrap \
  kodex-postgresql-runtime-credentials kodex-nats-credentials kodex-sentry \
  internal-rpc-authority-sentry \
  internal-rpc-authority-bootstrap-roots; do
  if [[ "$security_profile" == trusted-cluster && "$secret_name" == internal-rpc-authority-* ]]; then
    continue
  fi
  kubectl -n kodex-system get secret "$secret_name" -o json | jq -e \
    '.data | type == "object"' >/dev/null || fail "Secret readback failed: $secret_name"
done
if [[ "$security_profile" == protected ]]; then
secret_name=runtime-execution-client-tls
kubectl -n "$runtime_namespace" get secret "$secret_name" -o json | jq -e \
  '.data | type == "object"' >/dev/null || fail "runtime Secret readback failed: $secret_name"
fi
if [[ "$provider_mode" == configured ]]; then
python3 "$repository_root/tools/install/provider-bootstrap.py" verify-metadata \
  --context "$expected_context" >/dev/null || fail 'active provider credential readback failed'
fi
printf 'Kodex Kubernetes Secrets materialized: profile=%s provider=%s\n' "$security_profile" "$provider_mode"
