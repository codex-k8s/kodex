#!/usr/bin/env bash
set -euo pipefail
umask 077

fail() { printf 'Direct production application materialization failed: %s\n' "$*" >&2; exit 1; }
trap 'fail "unexpected command failure at line $LINENO"' ERR
usage() {
  printf 'Usage: %s --mode render|preflight|apply|readback --external-material-file <path> [--context <exact-context>] [--output <path>] [--nsc-bin <path>]\n' "$0" >&2
}

mode=""
expected_context=""
external_material_file=""
output=""
nsc_bin=""
while (($# > 0)); do
  case "$1" in
    --mode) mode="${2:-}"; shift 2 ;;
    --context) expected_context="${2:-}"; shift 2 ;;
    --external-material-file) external_material_file="${2:-}"; shift 2 ;;
    --output) output="${2:-}"; shift 2 ;;
    --nsc-bin) nsc_bin="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done
case "$mode" in render|preflight|apply|readback) ;; *) fail "mode must be render, preflight, apply or readback" ;; esac
[[ "$mode" == readback || -r "$external_material_file" ]] || fail "external material file is required"
[[ "$mode" != render || -n "$output" ]] || fail "render output is required"
[[ "$mode" == render || -n "$expected_context" ]] || fail "exact Kubernetes context is required"
for command_name in base64 jq kubectl node openssl sha256sum stat yq; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
if [[ -n "$expected_context" ]]; then
  [[ "$(kubectl config current-context)" == "$expected_context" ]] || fail "Kubernetes context mismatch"
fi

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repository_root=$(cd -- "$script_directory/../.." && pwd -P)
helper="$script_directory/direct-production-material-helper.mjs"
namespace=mattercodex-system
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT HUP INT TERM
policy="$temporary_directory/effective-policy.json"
jq -s '
  .[0] as $base | .[1] as $prototype |
  $base |
  .resources += ($prototype.runtime_owned_empty_resources |
    map({classification,kind,name,keys})) |
  .runtime_owned_empty_resources =
    (($base.publisher_owned_empty_resources | map(. + {owner:"publisher"})) +
      $prototype.runtime_owned_empty_resources) |
  .publisher_owned_empty_resources += ($prototype.runtime_owned_empty_resources |
    map(select(.owner == "publisher") | {kind,name,keys})) |
  .reconciler_owned_empty_resources = ($prototype.runtime_owned_empty_resources |
    map(select(.owner == "reconciler") | {kind,name,keys})) |
  .publisher_owned_runtime_keys = $prototype.publisher_owned_runtime_keys |
  .prototype_secret_backend = {
    deployment_profile:$prototype.deployment_profile,
    secret_backend:$prototype.secret_backend
  }
' "$repository_root/infra/direct-production/application-material-policy.json" \
  "$repository_root/infra/direct-production/internal-rpc-authority-prototype-material-policy.json" >"$policy"
jq -e '
  .schema_version == 1 and
  .prototype_secret_backend == {
    deployment_profile:"direct-production-single-node-prototype",
    secret_backend:"direct-production-kubernetes-file"
  } and
  ([.resources[] | [.kind,.name]] | length) ==
    ([.resources[] | [.kind,.name]] | unique | length) and
  all(.runtime_owned_empty_resources[];
    (.owner == "publisher" or .owner == "reconciler"))
' "$policy" >/dev/null || fail "application material policy is invalid"
values="$temporary_directory/values"
mkdir -p "$values/Secret" "$values/ConfigMap" "$temporary_directory/internal"

value_path() { printf '%s/%s/%s/%s' "$values" "$1" "$2" "$3"; }
has_value() { [[ -f "$(value_path "$1" "$2" "$3")" ]]; }
put_text() {
  local path
  path=$(value_path "$1" "$2" "$3")
  mkdir -p "$(dirname -- "$path")"
  printf '%s' "$4" >"$path"
  chmod 0600 "$path"
}
put_file() {
  local path
  path=$(value_path "$1" "$2" "$3")
  mkdir -p "$(dirname -- "$path")"
  cp "$4" "$path"
  chmod 0600 "$path"
}
random_hex_file() { openssl rand -hex 32 | tr -d '\n' >"$1"; chmod 0600 "$1"; }

expected_keys() {
  jq -c --arg kind "$1" --arg name "$2" 'first(.resources[] | select(.kind == $kind and .name == $name)).keys | sort' "$policy"
}
allowed_keys() {
  jq -c --arg kind "$1" --arg name "$2" '
    ((first(.resources[] | select(.kind == $kind and .name == $name)).keys) +
      ([.publisher_owned_runtime_keys[]? |
        select(.kind == $kind and .name == $name) | .keys[]])) |
    unique | sort
  ' "$policy"
}
runtime_owned_empty_key() {
  jq -e --arg kind "$1" --arg name "$2" --arg key "$3" '
    any(.runtime_owned_empty_resources[];
      .kind == $kind and .name == $name and (.keys | index($key) != null))
  ' "$policy" >/dev/null
}
verify_key_set_json() {
  local kind=$1 name=$2 json=$3 actual expected allowed
  expected=$(expected_keys "$kind" "$name")
  allowed=$(allowed_keys "$kind" "$name")
  actual=$(jq -c '[(.data // {} | keys[]),(.binaryData // {} | keys[])] | unique | sort' "$json")
  jq -e --argjson expected "$expected" --argjson allowed "$allowed" --argjson actual "$actual" '
    (($expected - $actual) | length) == 0 and (($actual - $allowed) | length) == 0
  ' <<<null >/dev/null || fail "$kind/$name has an unexpected key set"
}
load_resource_json() {
  local kind=$1 name=$2 json=$3 key path encoded
  verify_key_set_json "$kind" "$name" "$json"
  while IFS= read -r key; do
    path=$(value_path "$kind" "$name" "$key")
    mkdir -p "$(dirname -- "$path")"
    if [[ "$kind" == Secret ]]; then
      encoded=$(jq -er --arg key "$key" '.data[$key]' "$json") || fail "$kind/$name key is absent"
      printf '%s' "$encoded" | base64 -d >"$path" || fail "$kind/$name key is not base64"
    else
      jq -jr --arg key "$key" 'if .data[$key] != null then .data[$key] else empty end' "$json" >"$path"
      if [[ ! -s "$path" && "$(jq -r --arg key "$key" '.binaryData[$key] // empty' "$json")" != "" ]]; then
        jq -jr --arg key "$key" '.binaryData[$key]' "$json" | base64 -d >"$path"
      fi
    fi
    chmod 0600 "$path"
  done < <(jq -r '(.data // {} | keys[]),(.binaryData // {} | keys[])' "$json" | LC_ALL=C sort -u)
}
load_cluster_resource_if_present() {
  local kind=$1 name=$2
  local json="$temporary_directory/cluster-$kind-$name.json"
  [[ -n "$expected_context" ]] || return 1
  if kubectl --context "$expected_context" -n "$namespace" get "${kind,,}/$name" -o json >"$json" 2>/dev/null; then
    load_resource_json "$kind" "$name" "$json"
    return 0
  fi
  return 1
}
load_cluster_secret_key() {
  local source_namespace=$1 name=$2 key=$3 destination=$4
  local json="$temporary_directory/source-$source_namespace-$name.json"
  [[ -n "$expected_context" ]] || return 1
  if [[ ! -f "$json" ]]; then
    kubectl --context "$expected_context" -n "$source_namespace" get secret "$name" -o json >"$json" 2>/dev/null || return 1
  fi
  jq -er --arg key "$key" '.data[$key]' "$json" | base64 -d >"$destination" || return 1
  chmod 0600 "$destination"
}

if [[ "$mode" == readback ]]; then
  while IFS=$'\t' read -r kind name expected allowed; do
    json="$temporary_directory/readback-$kind-$name.json"
    kubectl --context "$expected_context" -n "$namespace" get "${kind,,}/$name" -o json >"$json" 2>/dev/null ||
      fail "$kind/$name is absent"
    actual=$(jq -c '[(.data // {} | keys[]),(.binaryData // {} | keys[])] | unique | sort' "$json")
    jq -e --argjson expected "$expected" --argjson allowed "$allowed" --argjson actual "$actual" '
      (($expected - $actual) | length) == 0 and (($actual - $allowed) | length) == 0
    ' <<<null >/dev/null || fail "$kind/$name readback key set mismatch"
    while IFS= read -r key; do
      [[ -n "$key" ]] || continue
      if runtime_owned_empty_key "$kind" "$name" "$key"; then
        continue
      fi
      jq -e --arg key "$key" '
        ((.data // {})[$key] // (.binaryData // {})[$key]) as $value |
        $value != null and ($value | type == "string" and length > 0)
      ' "$json" >/dev/null || fail "$kind/$name contains an empty key"
    done < <(jq -r '(.data // {} | keys[]),(.binaryData // {} | keys[])' "$json")
  done < <(jq -r '.resources[] | . as $resource |
    ([.publisher_owned_runtime_keys[]? |
      select(.kind == $resource.kind and .name == $resource.name) | .keys[]]) as $runtime |
    [.kind,.name,(.keys|sort|tojson),((.keys+$runtime)|unique|sort|tojson)] | @tsv' "$policy")
  for name in integration-gateway-provider-credentials interaction-gateway-bot-credentials; do
    jq -er '.data["state.json"]' "$temporary_directory/readback-Secret-$name.json" |
      base64 -d >"$temporary_directory/$name-state.json" || fail "Secret/$name aggregate readback is invalid"
    node "$helper" validate-aggregate "$temporary_directory/$name-state.json" 1024 >/dev/null
  done
  jq -er '.data["state.json"]' "$temporary_directory/readback-Secret-integration-gateway-git-credentials.json" |
    base64 -d >"$temporary_directory/integration-gateway-git-state.json" ||
    fail "Secret/integration-gateway-git-credentials readback is invalid"
  node "$helper" validate-git-aggregate "$temporary_directory/integration-gateway-git-state.json" \
    "$repository_root/deploy/k8s/base/integration-gateway/git-sources/catalog.json" >/dev/null
  for key in provider-snapshot.json provider-snapshot.sha256 provider-snapshot.generation; do
    jq -jr --arg key "$key" '.data[$key]' \
      "$temporary_directory/readback-ConfigMap-integration-gateway-oidc-provider.json" \
      >"$temporary_directory/$key" || fail "ConfigMap/integration-gateway-oidc-provider readback is invalid"
  done
  node "$helper" validate-oidc-snapshot "$temporary_directory/provider-snapshot.json" \
    "$temporary_directory/provider-snapshot.sha256" "$temporary_directory/provider-snapshot.generation" >/dev/null
  printf 'Direct production application material readback completed\n'
  exit 0
fi

classification="$temporary_directory/classification.json"
"$script_directory/classify-direct-production-application-material.sh" \
  --output "$classification" --external-material-file "$external_material_file" >/dev/null

# Preserve already materialized values before recomputing every derived binding.
if [[ -n "$expected_context" ]]; then
  while IFS=$'\t' read -r kind name; do
    load_cluster_resource_if_present "$kind" "$name" || true
  done < <(jq -r '.resources[] | [.kind,.name] | @tsv' "$policy")
fi

external_json="$temporary_directory/external.json"
yq -o=json eval-all '.' "$external_material_file" | jq -s '.' >"$external_json"
while IFS= read -r encoded_document; do
  json="$temporary_directory/external-document.json"
  printf '%s' "$encoded_document" | base64 -d >"$json"
  kind=$(jq -er '.kind' "$json")
  name=$(jq -er '.metadata.name' "$json")
  while IFS= read -r key; do
    path=$(value_path "$kind" "$name" "$key")
    mkdir -p "$(dirname -- "$path")"
    if [[ "$kind" == Secret ]]; then
      jq -jr --arg key "$key" '.data[$key]' "$json" | base64 -d >"$path"
    else
      jq -jr --arg key "$key" '.data[$key] // empty' "$json" >"$path"
      if [[ ! -s "$path" ]]; then
        jq -jr --arg key "$key" '.binaryData[$key] // empty' "$json" | base64 -d >"$path"
      fi
    fi
    chmod 0600 "$path"
  done < <(jq -r '(.data // {} | keys[]),(.binaryData // {} | keys[])' "$json")
done < <(jq -r '.[] | @base64' "$external_json")

# Validate external value semantics without logging their contents.
while IFS=$'\t' read -r kind name key; do
  path=$(value_path "$kind" "$name" "$key")
  [[ -s "$path" ]] || fail "external $kind/$name key is empty"
  case "$key" in
    *.jws|*.jwt) node "$helper" validate-jws "$path" >/dev/null ;;
    private.jwk) node "$helper" validate-private-jwk "$path" >/dev/null ;;
    *.jwk) jq -e 'type == "object" and .kty == "OKP" and .crv == "Ed25519" and (.x|type=="string" and length>0) and .d == null' "$path" >/dev/null || fail "external public JWK is invalid" ;;
    *public-keyset.json) jq -e '.keys|type=="array" and length>0 and all(.[];.kty=="OKP" and .crv=="Ed25519" and (.x|type=="string" and length>0) and .d==null)' "$path" >/dev/null || fail "external public keyset is invalid" ;;
    *.sha256) grep -Eq '^[a-f0-9]{64}$' "$path" || fail "external SHA-256 is invalid" ;;
    *-arn) grep -Eq '^arn:(aws|aws-cn|aws-us-gov):[a-z0-9-]+:[a-z0-9-]*:[0-9]{0,12}:.+$' "$path" || fail "external ARN is invalid" ;;
    ca.pem) openssl x509 -in "$path" -noout >/dev/null 2>&1 || fail "external CA is invalid" ;;
  esac
done < <(jq -r '.external_bindings[] | .kind as $kind | .name as $name | .keys[] | [$kind,$name,.] | @tsv' "$policy")
node "$helper" validate-authority-bootstrap \
  "$(value_path Secret internal-rpc-authority-publisher-manifest-signer private.jwk)" \
  "$(value_path Secret internal-rpc-authority-publisher-manifest-trust manifest-trust.jws)" \
  "$(value_path Secret internal-rpc-authority-publisher-readback-signer private.jwk)" \
  "$(value_path Secret internal-rpc-authority-readback-trust credential-trust.jws)"
mapping_manifest=$(value_path Secret interaction-gateway-mapping manifest.yaml)
mapping_digest=$(value_path Secret interaction-gateway-mapping manifest.sha256)
[[ "$(sha256sum "$mapping_manifest" | awk '{print $1}')" == "$(tr -d '\n' <"$mapping_digest")" ]] || fail "external mapping digest mismatch"
mapping_revision=$(tr -d '\n' <"$(value_path Secret interaction-gateway-mapping revision)")
[[ "$mapping_revision" =~ ^production-r[1-9][0-9]*$ ]] || fail "external mapping revision is invalid"
jq -e --arg revision "$mapping_revision" '
  def uuid: type == "string" and test("^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$");
  def provider_id: type == "string" and length > 0 and length <= 64;
  def stable_key: type == "string" and test("^[a-z0-9-]{1,64}$");
  . as $manifest |
  .version == 1 and .revision == $revision and
  (.source | type == "string" and startswith("vault://")) and
  (.channels | type == "array" and length > 0) and
  (.actors | type == "array" and length > 0) and
  (.bots | type == "array" and length > 0) and
  (any(.channels[]; .owner_delivery == true)) and
  ([.channels[] | [.team_id,.channel_id]] | length == (unique | length)) and
  ([.actors[] | [.mattermost_user_id,.organization_id,.project_id]] | length == (unique | length)) and
  ([.bots[].stable_key] | length == (unique | length)) and
  all(.channels[]; . as $channel |
    ($channel.team_id | provider_id) and
    ($channel.channel_id | provider_id) and
    ($channel.organization_id | uuid) and
    ($channel.project_id | uuid) and
    ($channel.chat_id | uuid) and
    ($channel.role_id | uuid) and
    ($channel.lifecycle_actor_id | uuid) and
    ($channel.locale == "ru" or $channel.locale == "en") and
    ($channel.bot_stable_key | stable_key) and
    ([$manifest.bots[].stable_key] | index($channel.bot_stable_key) != null) and
    ([$manifest.actors[] | select(
      .actor_id == $channel.lifecycle_actor_id and
      .organization_id == $channel.organization_id and
      .project_id == $channel.project_id)] | length == 1)) and
  all(.actors[];
    (.mattermost_user_id | provider_id) and
    (.actor_id | uuid) and
    (.organization_id | uuid) and
    (.project_id | uuid)) and
  all(.bots[];
    (.stable_key | stable_key) and
    (.user_id | provider_id) and
    (.token_file | type == "string" and startswith("/var/run/secrets/mattercodex/interaction-gateway/bots/")))
' "$mapping_manifest" >/dev/null || fail "external mapping manifest is invalid"
node "$helper" validate-git-aggregate \
  "$(value_path Secret integration-gateway-git-credentials state.json)" \
  "$repository_root/deploy/k8s/base/integration-gateway/git-sources/catalog.json" >/dev/null
node "$helper" validate-oidc-snapshot \
  "$(value_path ConfigMap integration-gateway-oidc-provider provider-snapshot.json)" \
  "$(value_path ConfigMap integration-gateway-oidc-provider provider-snapshot.sha256)" \
  "$(value_path ConfigMap integration-gateway-oidc-provider provider-snapshot.generation)" >/dev/null

# Dynamic aggregate Secrets начинаются с канонического пустого поколения
# и сохраняются при idempotent materialization без сброса runtime CAS state.
for name in integration-gateway-provider-credentials interaction-gateway-bot-credentials; do
  aggregate=$(value_path Secret "$name" state.json)
  if [[ -f "$aggregate" ]]; then
    node "$helper" validate-aggregate "$aggregate" 1024 >/dev/null
  else
    mkdir -p "$(dirname -- "$aggregate")"
    node "$helper" generate-empty-aggregate "$aggregate"
  fi
done

# Reusable bindings are copied only through exact owner-controlled bindings.
while IFS=$'\t' read -r target_kind target_name target_key source_namespace source_name source_key; do
  destination=$(value_path "$target_kind" "$target_name" "$target_key")
  mkdir -p "$(dirname -- "$destination")"
  if [[ -n "$expected_context" ]]; then
    load_cluster_secret_key "$source_namespace" "$source_name" "$source_key" "$destination" ||
      fail "reusable source binding is absent"
  elif [[ "$target_kind/$target_name/$target_key" == ConfigMap/mattermost-ca/ca.pem ]]; then
    : # populated from the offline prototype CA below
  else
    random_hex_file "$destination"
  fi
done < <(jq -r '.reusable_bindings[] | . as $binding | .key_map | to_entries[] |
  [$binding.target_kind,$binding.target_name,.key,$binding.source_namespace,$binding.source_name,.value] | @tsv' "$policy")

root="$temporary_directory/internal/root.hex"
root_json="$temporary_directory/internal/root.json"
if [[ -n "$expected_context" ]] && kubectl --context "$expected_context" -n "$namespace" \
  get secret mattercodex-application-material-root -o json >"$root_json" 2>/dev/null; then
  [[ "$(jq -c '[.data|keys[]]|sort' "$root_json")" == '["root.hex"]' ]] || fail "material root key set mismatch"
  jq -jr '.data["root.hex"]' "$root_json" | base64 -d | tr -d '\n' >"$root"
  [[ "$(wc -c <"$root")" -eq 64 ]] && grep -Eq '^[a-f0-9]{64}$' "$root" || fail "material root is invalid"
else
  random_hex_file "$root"
fi

ca_cert="$temporary_directory/internal/ca.crt"
ca_key="$temporary_directory/internal/ca.key"
if [[ -n "$expected_context" ]] &&
   load_cluster_secret_key "$namespace" mattercodex-prototype-ca ca.crt "$ca_cert" &&
   load_cluster_secret_key "$namespace" mattercodex-prototype-ca tls.key "$ca_key"; then
  openssl verify -CAfile "$ca_cert" "$ca_cert" >/dev/null 2>&1 || fail "prototype CA is invalid"
else
  openssl ecparam -name prime256v1 -genkey -noout -out "$ca_key" >/dev/null 2>&1
  openssl req -x509 -new -sha256 -key "$ca_key" -subj /CN=mattercodex-prototype-ca -days 30 -out "$ca_cert" >/dev/null 2>&1
fi
if ! has_value ConfigMap mattermost-ca ca.pem; then put_file ConfigMap mattermost-ca ca.pem "$ca_cert"; fi

# Foundation credentials remain separate from application resources.
redis_password="$temporary_directory/internal/redis-password"
object_user="$temporary_directory/internal/object-user"
object_password="$temporary_directory/internal/object-password"
if [[ -n "$expected_context" ]]; then
  load_cluster_secret_key "$namespace" mattercodex-redis-credentials password "$redis_password" || fail "foundation Redis credential is absent"
  load_cluster_secret_key "$namespace" mattercodex-object-store-credentials username "$object_user" || fail "foundation object-store username is absent"
  load_cluster_secret_key "$namespace" mattercodex-object-store-credentials password "$object_password" || fail "foundation object-store password is absent"
else
  random_hex_file "$redis_password"
  printf '%s' mattercodex >"$object_user"
  random_hex_file "$object_password"
fi

principal_names="$temporary_directory/internal/principals.txt"
yq -r '.data."principals.tsv"' deploy/k8s/base/direct-production-foundation/foundation.yaml 2>/dev/null |
  awk -F '\t' 'NF >= 4 && $1 ~ /^[a-z0-9_]+$/ {print $1}' | LC_ALL=C sort -u >"$principal_names"
principal_count=$(wc -l <"$principal_names" | tr -d ' ')
[[ "$principal_count" -eq 28 ]] || fail "PostgreSQL principal registry count is invalid: $principal_count"
principal_directory="$temporary_directory/internal/principals"
mkdir -p "$principal_directory"
principal_secret_json="$temporary_directory/internal/principals.json"
if [[ -n "$expected_context" ]] && kubectl --context "$expected_context" -n "$namespace" get secret mattercodex-postgresql-principals -o json >"$principal_secret_json" 2>/dev/null; then
  expected=$(jq -Rsc 'split("\n")|map(select(length>0))|sort' "$principal_names")
  actual=$(jq -c '[.data|keys[]]|sort' "$principal_secret_json")
  [[ "$actual" == "$expected" ]] || fail "PostgreSQL principal password key set mismatch"
  while IFS= read -r principal; do
    jq -jr --arg key "$principal" '.data[$key]' "$principal_secret_json" | base64 -d >"$principal_directory/$principal"
    grep -Eq '^[a-f0-9]{64}$' "$principal_directory/$principal" || fail "PostgreSQL principal password is invalid"
  done <"$principal_names"
else
  while IFS= read -r principal; do random_hex_file "$principal_directory/$principal"; done <"$principal_names"
fi

postgres_value() {
  local principal=$1 database=$2 ca_path=$3 role=${4:-} password query service
  password=$(<"$principal_directory/$principal")
  query="sslmode=verify-full&sslrootcert=$ca_path"
  [[ -z "$role" ]] || query="$query&options=-c%20role%3D$role"
  service=${database//_/-}
  printf 'postgresql://%s:%s@%s-postgresql-rw.mattercodex-system.svc.cluster.local:5432/%s?%s' "$principal" "$password" "$service" "$database" "$query"
}
put_pg() {
  local resource=$1 key=$2 principal=$3 database=$4 ca_path=$5 role=${6:-}
  put_text Secret "$resource" "$key" "$(postgres_value "$principal" "$database" "$ca_path" "$role")"
}
cp_ca=/var/run/config/mattercodex/control-plane/postgres/ca.pem
ig_ca=/var/run/config/mattercodex/integration-gateway/postgres/ca.pem
ix_ca=/var/run/config/mattercodex/interaction-gateway/postgres/ca.pem
rt_ca=/var/run/config/mattercodex/runtime-controller/postgres/ca.pem
ira_ca=/var/run/config/mattercodex/internal-rpc-authority/postgresql/ca.pem
put_pg control-plane-postgres-migration dsn control_plane_migrator control_plane "$cp_ca"
put_pg control-plane-postgres-migration runtime-current-dsn control_plane_runtime_g1 control_plane "$cp_ca" control_plane_runtime
put_pg control-plane-postgres-runtime dsn control_plane_runtime_g1 control_plane "$cp_ca" control_plane_runtime
put_pg control-plane-postgres-relay dsn control_plane_relay_g1 control_plane "$cp_ca" control_plane_relay
put_pg integration-gateway-postgres-migrator dsn integration_gateway_migrator_g1 integration_gateway "$ig_ca"
put_pg integration-gateway-postgres-migrator runtime-current-dsn integration_gateway_runtime_g1 integration_gateway "$ig_ca" integration_gateway_runtime
put_pg integration-gateway-postgres-migrator runtime-next-dsn integration_gateway_runtime_g2 integration_gateway "$ig_ca" integration_gateway_runtime
put_pg integration-gateway-postgres-runtime dsn integration_gateway_runtime_g1 integration_gateway "$ig_ca" integration_gateway_runtime
put_pg interaction-gateway-postgres-migrator dsn interaction_gateway_migrator interaction_gateway "$ix_ca"
put_pg interaction-gateway-runtime postgres-dsn interaction_gateway_runtime_g1 interaction_gateway "$ix_ca" interaction_gateway_runtime
put_pg runtime-controller-postgres-migration dsn runtime_controller_migrator runtime_controller "$rt_ca"
put_pg runtime-controller-postgres dsn runtime_controller_runtime_g1 runtime_controller "$rt_ca"
put_pg runtime-workload-admission-postgres dsn runtime_workload_admission_g1 runtime_controller "$rt_ca"
put_pg internal-rpc-authority-migrator-postgresql dsn internal_rpc_authority_migrator internal_rpc_authority "$ira_ca"
# Authority runtime проверяет исходный LOGIN principal и только затем сам
# активирует capability role через AfterConnect. DSN не должен делать SET ROLE.
put_pg internal-rpc-authority-database-credential-reconciler-postgresql dsn ira_database_credential_reconciler internal_rpc_authority "$ira_ca"
put_pg internal-rpc-authority-restore-controller-postgresql dsn ira_restore_controller_g1 internal_rpc_authority "$ira_ca"
for mapping in \
  automation-scheduler:ira_automation_scheduler_issuer_g1 \
  control-api-gateway:ira_control_api_gateway_issuer_g1 \
  integration-gateway:ira_integration_gateway_issuer_g1 \
  interaction-gateway:ira_interaction_gateway_issuer_g1 \
  runtime-controller:ira_runtime_controller_issuer_g1; do
  component=${mapping%%:*}; principal=${mapping#*:}
  resource="internal-rpc-authority-$component-issuer-postgresql"
  put_pg "$resource" dsn "$principal" internal_rpc_authority "$ira_ca"
  put_text Secret "$resource" username "$principal"
done
for mapping in control-plane:ira_control_plane_verifier_g1 integration-gateway:ira_integration_gateway_verifier_g1 interaction-gateway:ira_interaction_gateway_verifier_g1; do
  component=${mapping%%:*}; principal=${mapping#*:}
  resource="internal-rpc-authority-$component-verifier-postgresql"
  put_pg "$resource" dsn "$principal" internal_rpc_authority "$ira_ca"
  put_text Secret "$resource" username "$principal"
done
for generation in 3 4 5; do
  put_text Secret "internal-rpc-authority-publisher-database-g$generation" username "ira_publisher_g$generation"
  put_file Secret "internal-rpc-authority-publisher-database-g$generation" password "$principal_directory/ira_publisher_g$generation"
  put_text Secret "internal-rpc-authority-readback-database-g$generation" username "ira_readback_attestor_g$generation"
  put_file Secret "internal-rpc-authority-readback-database-g$generation" password "$principal_directory/ira_readback_attestor_g$generation"
done

# Derived foundation and protocol material.
put_file Secret control-plane-redis password "$redis_password"
put_file Secret control-plane-instruction-object-store access-key "$object_user"
put_file Secret control-plane-instruction-object-store secret-key "$object_password"
put_file Secret interaction-gateway-runtime s3-access-key "$object_user"
put_file Secret interaction-gateway-runtime s3-secret-key "$object_password"
put_file Secret integration-gateway-provider-health-credential provider-health.token "$(value_path Secret integration-gateway-provider-health-credential token)"
for item in \
  'ConfigMap control-plane-postgresql-ca ca.pem' 'ConfigMap control-plane-redis-ca ca.pem' \
  'ConfigMap integration-egress-proxy-ca ca.pem' 'ConfigMap internal-rpc-authority-database-credential-reconciler-client-ca client-ca.pem' \
  'ConfigMap internal-rpc-authority-postgresql-ca ca.pem' 'ConfigMap internal-rpc-authority-publisher-ca ca.pem' \
  'ConfigMap internal-rpc-authority-publisher-client-ca client-ca.pem' 'ConfigMap internal-rpc-authority-readback-attestor-ca ca.pem' \
  'ConfigMap internal-rpc-authority-readback-attestor-client-ca client-ca.pem' 'ConfigMap internal-rpc-authority-restore-controller-ca ca.pem' \
  'ConfigMap internal-rpc-authority-restore-controller-client-ca client-ca.pem' 'ConfigMap mattercodex-internal-ca ca.pem' \
  'ConfigMap mattercodex-nats-ca ca.pem' 'ConfigMap object-store-ca ca.pem' \
  'ConfigMap provider-health-adapter-ca ca.pem' 'ConfigMap runtime-controller-postgresql-ca ca.pem' \
  'Secret integration-gateway-postgresql-ca ca.crt' 'Secret interaction-gateway-postgresql-ca ca.crt'; do
  read -r kind name key <<<"$item"; put_file "$kind" "$name" "$key" "$ca_cert"
done
# Preserve private protocol keys, generating only absent ones.
while IFS=$'\t' read -r name key; do
  if has_value Secret "$name" "$key"; then
    node "$helper" validate-private-jwk "$(value_path Secret "$name" "$key")" >/dev/null ||
      fail "existing Secret/$name/$key is not a canonical private ES256 JWK"
  else
    path=$(value_path Secret "$name" "$key"); mkdir -p "$(dirname -- "$path")"
    node "$helper" generate-jwk "$path"
  fi
done < <(jq -r '.resources[] | select(.kind=="Secret") | .name as $name | .keys[] |
  select(. == "private.jwk" or . == "next-private.jwk" or . == "evidence-private.jwk" or . == "mattermost-event.private.jwk" or . == "provider-readback.private.jwk") | [$name,.] | @tsv' "$policy")
public_jwks() {
  local destination
  destination=$(value_path Secret "$1" "$2"); mkdir -p "$(dirname -- "$destination")"
  node "$helper" public-jwks "$destination" "$(value_path Secret "$3" "$4")"
}
public_keyset_genesis() {
  local destination public_keyset uncanonical
  destination=$(value_path Secret "$1" "$2"); mkdir -p "$(dirname -- "$destination")"
  public_keyset="$temporary_directory/$1-$2.jwks.json"
  uncanonical="$temporary_directory/$1-$2.genesis.json"
  node "$helper" public-jwks "$public_keyset" "$(value_path Secret "$3" "$4")"
  jq -e '
    (.keys | type == "array" and length == 1) as $valid |
    if $valid then {
      version: 1,
      revision: 1,
      high_watermark: 1,
      served_generation: 1,
      keys: [.keys[] | {generation: 1, status: "CURRENT", jwk: .}]
    } else error("public keyset genesis requires exactly one key") end
  ' "$public_keyset" >"$uncanonical"
  node "$helper" canonicalize-json "$uncanonical" "$destination"
}
public_jwk() {
  local destination
  destination=$(value_path Secret "$1" "$2"); mkdir -p "$(dirname -- "$destination")"
  node "$helper" public-jwk "$(value_path Secret "$3" "$4")" "$destination"
}
public_jwks control-plane-interaction-readback-trust public-keyset.json control-plane-interaction-readback-signer private.jwk
public_keyset_genesis control-plane-keyset-genesis interaction-readback.public-keyset.json control-plane-interaction-readback-signer private.jwk
public_keyset_genesis control-plane-keyset-genesis mattermost-event.public-keyset.json interaction-gateway-runtime mattermost-event.private.jwk
public_jwks interaction-gateway-runtime delivery-readback.public-keyset.json control-plane-interaction-readback-signer private.jwk
public_keyset_genesis interaction-gateway-postgres-migrator delivery-readback.public-keyset.json control-plane-interaction-readback-signer private.jwk
for component in automation-scheduler control-api-gateway integration-gateway interaction-gateway runtime-controller; do
  public_jwks "internal-rpc-authority-$component-proof-trust" jwks.json "internal-rpc-authority-$component-issuer-key" private.jwk
done
public_jwks internal-rpc-authority-control-plane-resolver-trust jwks.json internal-rpc-authority-control-plane-resolver-key private.jwk
public_jwk internal-rpc-authority-restore-role-trust evidence-public.jwk internal-rpc-authority-restore-pitr-evidence-signer evidence-private.jwk
put_file Secret internal-rpc-authority-restore-role-trust manifest-trust.jws \
  "$(value_path Secret internal-rpc-authority-publisher-manifest-trust manifest-trust.jws)"
node "$helper" generate-restore-role-trust \
  "$(value_path Secret internal-rpc-authority-restore-role-trust restore-role-trust.jws)" \
  "$(value_path Secret internal-rpc-authority-publisher-manifest-signer private.jwk)" \
  "$(value_path Secret internal-rpc-authority-publisher-signers private.jwk)" \
  "$(value_path Secret internal-rpc-authority-publisher-signers next-private.jwk)" \
  1 1 31536000
mkdir -p "$(dirname -- "$(value_path Secret integration-gateway-payload-keyset keyset.json)")"
node "$helper" generate-payload-keyset "$root" "$(value_path Secret integration-gateway-payload-keyset keyset.json)"

for key in admission-private-key.hex; do
  if ! has_value Secret control-plane-runtime-workload-signing "$key"; then
    path=$(value_path Secret control-plane-runtime-workload-signing "$key"); mkdir -p "$(dirname -- "$path")"
    node "$helper" derive-hex "$root" "control-plane-runtime-workload-signing/$key" "$path"
  fi
done
ticket_public() {
  local destination
  destination=$(value_path Secret "$2" "$3"); mkdir -p "$(dirname -- "$destination")"
  node "$helper" ed25519-public-hex "$(value_path Secret control-plane-runtime-workload-signing "$1")" "$destination"
}
ticket_public admission-private-key.hex runtime-workload-admission-ticket-trust public-key.hex
ticket_public admission-private-key.hex runtime-workload-materializer-ticket-trust public-key.hex

# Generate exact client/server certificates from the owner-controlled prototype CA.
generate_tls() {
  local name=$1 cert key ca service service_account san_file csr serial cert_text
  cert=$(value_path Secret "$name" tls.crt); key=$(value_path Secret "$name" tls.key); ca=$(value_path Secret "$name" ca.crt)
  service=${name%-tls}; service=${service%-server}; service=${service%-client}; service=${service%-workload}
  service_account=$service
  case "$name" in
    control-api-gateway-public-tls-material) service=control-api.mattercodex.local; service_account=control-api-gateway ;;
    integration-egress-proxy-server-tls) service=integration-egress-proxy; service_account=integration-egress-proxy ;;
    integration-egress-proxy-provider-client-tls) service_account=integration-egress-proxy ;;
    provider-health-adapter-server-tls) service=provider-health-adapter; service_account=provider-health-adapter ;;
    runtime-controller-nats-tls) service_account=runtime-controller ;;
    internal-rpc-authority-*-workload-tls)
      service_account=${name#internal-rpc-authority-}; service_account=${service_account%-workload-tls} ;;
    internal-rpc-authority-restore-operator-tls) service_account=internal-rpc-authority-restore-operator ;;
  esac
  if [[ -s "$cert" && -s "$key" && -s "$ca" ]]; then
    openssl verify -CAfile "$ca" "$cert" >/dev/null 2>&1 || fail "Secret/$name TLS chain is invalid"
    if [[ "$name" == internal-rpc-authority-*-workload-tls || "$name" == runtime-controller-nats-tls ]]; then
      cert_text="$temporary_directory/$name.text"; openssl x509 -in "$cert" -noout -text >"$cert_text"
      grep -Fq "URI:spiffe://mattercodex.local/ns/$namespace/sa/$service_account" "$cert_text" || fail "Secret/$name SPIFFE identity is invalid"
    fi
    return
  fi
  mkdir -p "$(dirname -- "$cert")"
  san_file="$temporary_directory/$name.ext"; csr="$temporary_directory/$name.csr"; serial="$temporary_directory/$name.srl"
  printf 'subjectAltName=DNS:%s,DNS:%s.%s.svc,DNS:%s.%s.svc.cluster.local,URI:spiffe://mattercodex.local/ns/%s/sa/%s\nextendedKeyUsage=serverAuth,clientAuth\n' \
    "$service" "$service" "$namespace" "$service" "$namespace" "$namespace" "$service_account" >"$san_file"
  openssl ecparam -name prime256v1 -genkey -noout -out "$key" >/dev/null 2>&1
  openssl req -new -sha256 -key "$key" -subj "/CN=$service" -out "$csr" >/dev/null 2>&1
  openssl x509 -req -sha256 -in "$csr" -CA "$ca_cert" -CAkey "$ca_key" -CAserial "$serial" -CAcreateserial -days 90 -extfile "$san_file" -out "$cert" >/dev/null 2>&1
  cp "$ca_cert" "$ca"; chmod 0600 "$cert" "$key" "$ca"
  openssl verify -CAfile "$ca" "$cert" >/dev/null 2>&1 || fail "Secret/$name generated TLS chain is invalid"
  openssl x509 -in "$cert" -noout -checkhost "$service.$namespace.svc.cluster.local" >/dev/null 2>&1 || fail "Secret/$name TLS hostname is invalid"
  cert_text="$temporary_directory/$name.text"; openssl x509 -in "$cert" -noout -text >"$cert_text"
  grep -Fq "URI:spiffe://mattercodex.local/ns/$namespace/sa/$service_account" "$cert_text" || fail "Secret/$name generated SPIFFE identity is invalid"
}
while IFS= read -r name; do generate_tls "$name"; done < <(jq -r '.resources[] | select(.kind=="Secret" and (.keys|index("tls.crt"))) | .name' "$policy")

material_json=$(value_path Secret control-api-gateway-public-tls-material material.json)
cert_der="$temporary_directory/control-api.der"
openssl x509 -in "$(value_path Secret control-api-gateway-public-tls-material tls.crt)" -outform DER -out "$cert_der"
openssl x509 -in "$(value_path Secret control-api-gateway-public-tls-material tls.crt)" -noout \
  -checkhost control-api.mattercodex.local >/dev/null 2>&1 || fail "control API public TLS hostname is invalid"
jq -n --arg digest "$(sha256sum "$cert_der" | awk '{print $1}')" \
  '{generation:1,certificateSha256:$digest,predecessorGeneration:0,predecessorCertificateSha256:""}' \
  >"$material_json"

# NATS account and users are generated as one atomic set; a partial live set is rejected.
nats_existing=0
for name in mattercodex-nats-credentials control-plane-nats runtime-controller-nats; do
  if [[ -n "$expected_context" ]] && kubectl --context "$expected_context" -n "$namespace" get secret "$name" >/dev/null 2>&1; then
    nats_existing=$((nats_existing + 1))
  fi
done
if [[ "$nats_existing" -eq 3 ]]; then
  nats_server_json="$temporary_directory/nats-server.json"
  kubectl --context "$expected_context" -n "$namespace" get secret mattercodex-nats-credentials -o json >"$nats_server_json"
  [[ "$(jq -c '[.data|keys[]]|sort' "$nats_server_json")" == '["account.jwt","account.public","operator.jwt","system-account.jwt","system-account.public"]' ]] || fail "NATS server credential key set mismatch"
  for key in operator.jwt system-account.jwt system-account.public account.jwt account.public; do
    jq -jr --arg key "$key" '.data[$key]' "$nats_server_json" | base64 -d >"$temporary_directory/internal/$key"
  done
elif [[ "$nats_existing" -ne 0 ]]; then
  fail "NATS credential set is partially materialized"
else
  if [[ -z "$nsc_bin" ]]; then
    nsc_bin="$temporary_directory/nsc"
    "$script_directory/install-pinned-nsc.sh" "$nsc_bin" >/dev/null
  fi
  [[ -x "$nsc_bin" ]] || fail "nsc binary is not executable"
  export NSC_HOME="$temporary_directory/nsc-home" NKEYS_PATH="$temporary_directory/nkeys"
  "$nsc_bin" -H "$NSC_HOME" add operator -n mattercodex --sys >/dev/null
  "$nsc_bin" -H "$NSC_HOME" add account -n applications >/dev/null
  "$nsc_bin" -H "$NSC_HOME" edit account -n applications --js-mem-storage 536870912 --js-disk-storage 8589934592 --js-streams 8 --js-consumer 32 >/dev/null
  "$nsc_bin" -H "$NSC_HOME" add user -a applications -n control-plane \
    --allow-pub '$JS.API.>,control_plane.runtime_configuration_changed' --allow-sub '_INBOX.>' >/dev/null
  "$nsc_bin" -H "$NSC_HOME" add user -a applications -n runtime-controller \
    --allow-pub '$JS.API.>,$JS.ACK.>' --allow-sub '_INBOX.>,control_plane.runtime_configuration_changed' >/dev/null
  mkdir -p "$(dirname -- "$(value_path Secret control-plane-nats user.creds)")" \
    "$(dirname -- "$(value_path Secret runtime-controller-nats user.creds)")"
  "$nsc_bin" -H "$NSC_HOME" generate creds -a applications -n control-plane -o "$(value_path Secret control-plane-nats user.creds)" >/dev/null 2>&1
  "$nsc_bin" -H "$NSC_HOME" generate creds -a applications -n runtime-controller -o "$(value_path Secret runtime-controller-nats user.creds)" >/dev/null 2>&1
  "$nsc_bin" -H "$NSC_HOME" describe operator -n mattercodex -R -o "$temporary_directory/internal/operator.decorated" >/dev/null
  "$nsc_bin" -H "$NSC_HOME" describe account -n SYS -R -o "$temporary_directory/internal/system-account.decorated" >/dev/null
  "$nsc_bin" -H "$NSC_HOME" describe account -n applications -R -o "$temporary_directory/internal/account.decorated" >/dev/null
  node "$helper" extract-jwt "$temporary_directory/internal/operator.decorated" "$temporary_directory/internal/operator.jwt"
  node "$helper" extract-jwt "$temporary_directory/internal/system-account.decorated" "$temporary_directory/internal/system-account.jwt"
  node "$helper" extract-jwt "$temporary_directory/internal/account.decorated" "$temporary_directory/internal/account.jwt"
  "$nsc_bin" -H "$NSC_HOME" describe account -n SYS -J | jq -jer '.sub' >"$temporary_directory/internal/system-account.public"
  "$nsc_bin" -H "$NSC_HOME" describe account -n applications -J | jq -jer '.sub' >"$temporary_directory/internal/account.public"
fi
for name in control-plane-nats runtime-controller-nats; do
  creds=$(value_path Secret "$name" user.creds)
  if [[ "$name" == control-plane-nats ]]; then
    node "$helper" validate-nats-creds "$creds" control-plane \
      '$JS.API.>,control_plane.runtime_configuration_changed' '_INBOX.>'
  else
    node "$helper" validate-nats-creds "$creds" runtime-controller \
      '$JS.ACK.>,$JS.API.>' '_INBOX.>,control_plane.runtime_configuration_changed'
  fi
done
node "$helper" validate-nats-server "$temporary_directory/internal/operator.jwt" \
  "$temporary_directory/internal/system-account.jwt" "$temporary_directory/internal/system-account.public" \
  "$temporary_directory/internal/account.jwt" "$temporary_directory/internal/account.public"

# Fill remaining symmetric values through domain-separated HKDF; unknown interfaces fail closed.
while IFS=$'\t' read -r kind name key; do
  has_value "$kind" "$name" "$key" && continue
  path=$(value_path "$kind" "$name" "$key"); mkdir -p "$(dirname -- "$path")"
  if runtime_owned_empty_key "$kind" "$name" "$key"; then
    : >"$path"
    continue
  fi
  case "$kind/$name/$key" in
    Secret/*/*.hex|Secret/*/key|Secret/*/callback-key|Secret/*/delivery-key)
      node "$helper" derive-hex "$root" "$name/$key" "$path" ;;
    *) fail "no material source for $kind/$name/$key" ;;
  esac
done < <(jq -r '.resources[] | .kind as $kind | .name as $name | .keys[] | [$kind,$name,.] | @tsv' "$policy")

# Exact closure и закрытый набор пустых ключей publisher.
while IFS=$'\t' read -r kind name key; do
  path=$(value_path "$kind" "$name" "$key")
  [[ -f "$path" ]] || fail "$kind/$name/$key was not materialized"
  if ! runtime_owned_empty_key "$kind" "$name" "$key"; then
    [[ -s "$path" ]] || fail "$kind/$name/$key is empty"
  fi
done < <(jq -r '.resources[] | .kind as $kind | .name as $name | .keys[] | [$kind,$name,.] | @tsv' "$policy")

render="$temporary_directory/application-material.yaml"
: >"$render"
while IFS=$'\t' read -r kind name; do
  args=()
  while IFS= read -r key; do
    [[ -f "$(value_path "$kind" "$name" "$key")" ]] || continue
    args+=("--from-file=$key=$(value_path "$kind" "$name" "$key")")
  done < <(jq -r --arg kind "$kind" --arg name "$name" '
    first(.resources[]|select(.kind==$kind and .name==$name)) as $resource |
    (($resource.keys + [.publisher_owned_runtime_keys[]? |
      select(.kind==$kind and .name==$name) | .keys[]]) | unique[])
  ' "$policy")
  if [[ "$kind" == Secret ]]; then
    kubectl -n "$namespace" create secret generic "$name" "${args[@]}" --dry-run=client -o yaml >>"$render"
  else
    kubectl -n "$namespace" create configmap "$name" "${args[@]}" --dry-run=client -o yaml >>"$render"
  fi
  printf '%s\n' '---' >>"$render"
done < <(jq -r '.resources[] | [.kind,.name] | @tsv' "$policy")

if [[ "$mode" == render ]]; then
  output_directory=$(dirname -- "$output"); [[ -d "$output_directory" ]] || fail "output directory is absent"
  output_temporary=$(mktemp "$output_directory/.material.XXXXXX")
  cp "$render" "$output_temporary"; chmod 0600 "$output_temporary"; mv -f "$output_temporary" "$output"
  printf 'Direct production application material render created: %s\n' "$output"
  exit 0
fi

# Internal owner-only sources are dry-run validated or applied before the closed application set.
internal="$temporary_directory/internal.yaml"
: >"$internal"
kubectl -n "$namespace" create secret generic mattercodex-application-material-root --from-file=root.hex="$root" --dry-run=client -o yaml >>"$internal"
principal_args=(); while IFS= read -r principal; do principal_args+=("--from-file=$principal=$principal_directory/$principal"); done <"$principal_names"
printf '%s\n' '---' >>"$internal"
kubectl -n "$namespace" create secret generic mattercodex-postgresql-principals "${principal_args[@]}" --dry-run=client -o yaml >>"$internal"
if [[ "$nats_existing" -eq 0 ]]; then
  printf '%s\n' '---' >>"$internal"
  kubectl -n "$namespace" create secret generic mattercodex-nats-credentials \
    --from-file=operator.jwt="$temporary_directory/internal/operator.jwt" \
    --from-file=system-account.jwt="$temporary_directory/internal/system-account.jwt" \
    --from-file=system-account.public="$temporary_directory/internal/system-account.public" \
    --from-file=account.jwt="$temporary_directory/internal/account.jwt" \
    --from-file=account.public="$temporary_directory/internal/account.public" --dry-run=client -o yaml >>"$internal"
fi
kubectl --context "$expected_context" apply --dry-run=server -f "$internal" >/dev/null
kubectl --context "$expected_context" apply --dry-run=server -f "$render" >/dev/null
if [[ "$mode" == apply ]]; then
  kubectl --context "$expected_context" apply -f "$internal" >/dev/null
  kubectl --context "$expected_context" apply -f "$render" >/dev/null
  "$0" --mode readback --context "$expected_context"
else
  printf 'Direct production application material preflight completed\n'
fi
