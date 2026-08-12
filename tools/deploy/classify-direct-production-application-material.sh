#!/usr/bin/env bash
set -euo pipefail
umask 077

fail() {
  printf 'Application material classification failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf 'Usage: %s --output <classification.json> [--external-material-file <path>] [--context <exact-context>]\n' "$0" >&2
}

output=""
external_material_file=""
expected_context=""
while (($# > 0)); do
  case "$1" in
    --output) output="${2:-}"; shift 2 ;;
    --external-material-file) external_material_file="${2:-}"; shift 2 ;;
    --context) expected_context="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$output" ]] || fail "output path is required"
for command_name in jq kubectl yq; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
if [[ -n "$expected_context" ]]; then
  [[ "$(kubectl config current-context)" == "$expected_context" ]] ||
    fail "Kubernetes context mismatch"
fi

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repository_root=$(cd -- "$script_directory/../.." && pwd -P)
temporary_directory=$(mktemp -d)
output_temporary=""
cleanup() {
  rm -rf -- "$temporary_directory"
  [[ -z "$output_temporary" ]] || rm -f -- "$output_temporary"
}
trap cleanup EXIT
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
  . as $policy |
  .schema_version == 1 and
  .prototype_secret_backend == {
    deployment_profile:"direct-production-single-node-prototype",
    secret_backend:"direct-production-kubernetes-file"
  } and
  .profile == "direct-production single-node prototype" and
  .namespace == "mattercodex-system" and
  (.resources | type == "array" and length > 0) and
  (.resources | group_by([.kind,.name]) | all(length == 1)) and
  all(.resources[];
    (.kind == "Secret" or .kind == "ConfigMap") and
    (.name | test("^[a-z0-9]([-a-z0-9]*[a-z0-9])?$")) and
    (.keys | type == "array" and length > 0 and length == (unique | length) and
      all(.[]; type == "string" and length > 0)) and
    (.classification == "cryptographically_generated" or
     .classification == "deterministically_derived" or
     .classification == "safely_reusable_from_existing_binding" or
     .classification == "truly_external_credential")) and
  ([.external_bindings[],.reusable_bindings[],.runtime_owned_empty_resources[]] |
    all(.kind != null or .target_kind != null)) and
  all(.external_bindings[]; . as $binding |
    any($policy.resources[]; .kind == $binding.kind and .name == $binding.name and
      (($binding.keys - .keys) | length) == 0)) and
  all(.publisher_owned_empty_resources[]; . as $binding |
    any($policy.resources[]; .kind == $binding.kind and .name == $binding.name and
      .keys == $binding.keys)) and
  all(.reconciler_owned_empty_resources[]; . as $binding |
    any($policy.resources[]; .kind == $binding.kind and .name == $binding.name and
      .keys == $binding.keys)) and
  all(.runtime_owned_empty_resources[];
    (.owner == "publisher" or .owner == "reconciler")) and
  all(.publisher_owned_runtime_keys[]; . as $binding |
    any($policy.resources[]; .kind == $binding.kind and .name == $binding.name and
      (($binding.keys - .keys) | length) == ([$binding.keys[]] | length)))
' "$policy" >/dev/null || fail "application material policy is invalid"

authority_policy="$repository_root/deploy/k8s/base/internal-rpc-authority-publisher/authority-policy.json"
missing_application_grant_trust=$(
  jq -rn --slurpfile authority "$authority_policy" --slurpfile material "$policy" '
    ($material[0].resources[] |
      select(.kind == "Secret" and .name == "control-plane-application-grants") |
      .keys) as $declared |
    [
      $authority[0].policy.authority_proof_producers[] |
      select(.producer_id != "control-plane.oidc") |
      if (.application_credential == "MATTERMOST_SIGNED_EVENT" or
          .application_credential == "INTEGRATION_CONTINUATION_GRANT") then
        .producer_id + ".public-keyset.json"
      else
        .producer_id + ".public.jwk"
      end as $required |
      select(($declared | index($required)) == null) |
      $required
    ] | sort | join(",")
  '
)
[[ -z "$missing_application_grant_trust" ]] ||
  fail "control-plane application grant trust is incomplete: $missing_application_grant_trust"
interfaces="$temporary_directory/interfaces.yaml"
"$repository_root/tools/release/render-direct-production-applications.sh" --scope interfaces --output "$interfaces" >/dev/null

expected_secrets="$temporary_directory/expected-secrets"
{
  yq eval-all -r '.. | select(has("secretKeyRef")) | .secretKeyRef.name' "$interfaces"
  yq eval-all -r '.. | select(has("secret")) |
    select(.secret.secretName != null) | .secret.secretName' "$interfaces"
} | sed '/^---$/d;/^null$/d;/^$/d' | LC_ALL=C sort -u >"$expected_secrets"
{
  yq eval-all -r '.. | select(has("secretKeyRef")) |
    [.secretKeyRef.name,.secretKeyRef.key] | @tsv' "$interfaces"
  yq eval-all -r '.. | select(has("secret")) | select(.secret.secretName != null) |
    .secret.secretName as $name | .secret.items[]? | [$name,.key] | @tsv' "$interfaces"
} | sed '/^---$/d;/^null$/d;/^$/d' | LC_ALL=C sort -u >"$temporary_directory/referenced-secret-keys"

defined_configmaps="$temporary_directory/defined-configmaps"
referenced_configmaps="$temporary_directory/referenced-configmaps"
yq eval-all -r 'select(.kind == "ConfigMap") | .metadata.name' "$interfaces" |
  sed '/^---$/d;/^null$/d;/^$/d' | LC_ALL=C sort -u >"$defined_configmaps"
{
  yq eval-all -r '.. | select(has("configMapRef")) | .configMapRef.name' "$interfaces"
  yq eval-all -r '.. | select(has("configMapKeyRef")) | .configMapKeyRef.name' "$interfaces"
  yq eval-all -r '.. | select(has("configMap")) |
    select(.configMap.name != null) | .configMap.name' "$interfaces"
} | sed '/^---$/d;/^null$/d;/^$/d' | LC_ALL=C sort -u >"$referenced_configmaps"
{
  yq eval-all -r '.. | select(has("configMapKeyRef")) |
    [.configMapKeyRef.name,.configMapKeyRef.key] | @tsv' "$interfaces"
  yq eval-all -r '.. | select(has("configMap")) | select(.configMap.name != null) |
    .configMap.name as $name | .configMap.items[]? | [$name,.key] | @tsv' "$interfaces"
} | sed '/^---$/d;/^null$/d;/^$/d' | LC_ALL=C sort -u >"$temporary_directory/referenced-configmap-keys"
comm -13 "$defined_configmaps" "$referenced_configmaps" |
  sed '/^kube-root-ca\.crt$/d;/^mattercodex-image-admission-policy$/d' >"$temporary_directory/expected-configmaps"

resources="$temporary_directory/resources.json"
printf '[]' >"$resources"
while IFS=$'\t' read -r kind name; do
  [[ -n "$kind" && -n "$name" ]] || continue
  resource=$(jq -cer --arg kind "$kind" --arg name "$name" '
    first(.resources[] | select(.kind == $kind and .name == $name)) |
    {kind,name,classification,keys}
  ' "$policy") || fail "rendered application material is absent from the closed policy"
  jq --argjson resource "$resource" '. + [$resource]' "$resources" >"$resources.next"
  mv "$resources.next" "$resources"
done < <(
  sed 's/^/Secret\t/' "$expected_secrets"
  sed 's/^/ConfigMap\t/' "$temporary_directory/expected-configmaps"
)

# Publisher/reconciler управляют только этими заранее объявленными пустыми
# ресурсами. RBAC не даёт create, поэтому их создаёт owner materializer.
while IFS=$'\t' read -r kind name classification keys; do
  jq -e --arg kind "$kind" --arg name "$name" '
    any(.[]; .kind == $kind and .name == $name)
  ' "$resources" >/dev/null && continue
  jq --arg kind "$kind" --arg name "$name" --arg classification "$classification" \
    --argjson keys "$keys" \
    '. + [{kind:$kind,name:$name,classification:$classification,keys:$keys}]' \
    "$resources" >"$resources.next"
  mv "$resources.next" "$resources"
done < <(jq -r '. as $policy | .runtime_owned_empty_resources[] | . as $binding |
  first($policy.resources[] | select(.kind == $binding.kind and .name == $binding.name)) |
  [.kind,.name,.classification,(.keys | tojson)] | @tsv' "$policy")

# API-backed aggregates задаются закрытым backend, а не Secret volume.
# Два exact ресурса допускаются только после проверки итогового
# выбора профиля и имён реестра.
yq -o=json eval-all '.' "$interfaces" | jq -s -e '
  any(.[]; .kind == "ConfigMap" and .metadata.name == "integration-gateway-runtime" and
    .data.INTEGRATION_GATEWAY_DEPLOYMENT_PROFILE == "direct-production-single-node-prototype" and
    .data.INTEGRATION_GATEWAY_SECRET_BACKEND == "direct-production-kubernetes-file" and
    .data.INTEGRATION_GATEWAY_KUBERNETES_PROVIDER_SECRET_NAME == "integration-gateway-provider-credentials" and
    .data.INTEGRATION_GATEWAY_KUBERNETES_PROVIDER_SECRET_DATA_KEY == "state.json") and
  any(.[]; .kind == "ConfigMap" and .metadata.name == "interaction-gateway-runtime" and
    .data.INTERACTION_GATEWAY_DEPLOYMENT_PROFILE == "direct-production-single-node-prototype" and
    .data.INTERACTION_GATEWAY_BOT_CREDENTIAL_BACKEND == "direct-production-kubernetes-file" and
    .data.INTERACTION_GATEWAY_BOT_CREDENTIAL_KUBERNETES_RESOURCE_NAME == "interaction-gateway-bot-credentials" and
    .data.INTERACTION_GATEWAY_BOT_CREDENTIAL_KUBERNETES_DATA_KEY == "state.json")
' >/dev/null || fail "direct runtime aggregate registry is not exact"
for name in integration-gateway-provider-credentials interaction-gateway-bot-credentials; do
  resource=$(jq -cer --arg name "$name" '
    first(.resources[] | select(.kind == "Secret" and .name == $name)) |
    {kind,name,classification,keys}
  ' "$policy") || fail "direct runtime aggregate is absent from the closed policy"
  jq --argjson resource "$resource" '. + [$resource]' "$resources" >"$resources.next"
  mv "$resources.next" "$resources"
done

rendered_resource_identities=$(jq -Sc '[.[] | [.kind,.name]] | sort' "$resources")
policy_resource_identities=$(jq -Sc '[.resources[] | [.kind,.name]] | sort' "$policy")
[[ "$rendered_resource_identities" == "$policy_resource_identities" ]] ||
  fail "rendered application material identity set differs from the closed policy"
while IFS=$'\t' read -r name key; do
  jq -e --arg name "$name" --arg key "$key" '
    any(.resources[]; .kind == "Secret" and .name == $name and (.keys | index($key) != null))
  ' "$policy" >/dev/null || fail "rendered Secret key is absent from the closed policy"
done <"$temporary_directory/referenced-secret-keys"
while IFS=$'\t' read -r name key; do
  jq -e --arg name "$name" --arg key "$key" '
    if any(.resources[]; .kind == "ConfigMap" and .name == $name)
    then any(.resources[]; .kind == "ConfigMap" and .name == $name and (.keys | index($key) != null))
    else true end
  ' "$policy" >/dev/null || fail "rendered ConfigMap key is absent from the closed policy"
done <"$temporary_directory/referenced-configmap-keys"

jq -e '
  length > 0 and
  (group_by([.kind,.name]) | all(length == 1)) and
  all(.[];
    (.kind == "Secret" or .kind == "ConfigMap") and
    (.name | test("^[a-z0-9]([-a-z0-9]*[a-z0-9])?$")) and
    (.keys | type == "array" and length > 0 and length == (unique | length) and
      all(.[]; type == "string" and length > 0)) and
    (.classification == "cryptographically_generated" or
     .classification == "deterministically_derived" or
     .classification == "safely_reusable_from_existing_binding" or
     .classification == "truly_external_credential"))
' "$resources" >/dev/null || fail "resource classification is not exact"

if [[ -n "$external_material_file" ]]; then
  [[ -r "$external_material_file" ]] || fail "external material file is not readable"
  external_material_mode=$(stat -c '%a' "$external_material_file")
  (( (8#$external_material_mode & 077) == 0 )) ||
    fail "external material file permissions expose credential data"
  external_json="$temporary_directory/external.json"
  yq -o=json eval-all '.' "$external_material_file" | jq -s '.' >"$external_json"
  jq -e '
    all(.[]; type == "object" and
      .apiVersion == "v1" and
      (.kind == "Secret" or .kind == "ConfigMap") and
      .metadata.namespace == "mattercodex-system" and
      (if .kind == "Secret"
       then (.data | type == "object" and length > 0) and .stringData == null
       else (((((.data // {}) | keys) - ((.binaryData // {}) | keys)) | length) ==
             ((.data // {}) | length)) and
            ((((.data // {}) | length) + ((.binaryData // {}) | length)) > 0)
       end))
  ' "$external_json" >/dev/null || fail "external material file contains an invalid resource"
  expected_external_identities=$(jq -Sc '
    [.external_bindings[] | [.kind,.name]] | sort
  ' "$policy")
  actual_external_identities=$(jq -Sc '
    [.[] | [.kind,.metadata.name]] | sort
  ' "$external_json")
  [[ "$actual_external_identities" == "$expected_external_identities" ]] ||
    fail "external material resource identity set mismatch"
  while IFS=$'\t' read -r kind name key; do
    [[ -n "$kind" && -n "$name" && -n "$key" ]] || continue
    KIND="$kind" NAME="$name" KEY="$key" jq -e '
        any(.[];
          .kind == env.KIND and .metadata.name == env.NAME and
          (if env.KIND == "Secret"
           then (.data[env.KEY] | type == "string" and length > 0)
           else (((.data // {})[env.KEY] // (.binaryData // {})[env.KEY]) |
             type == "string" and length > 0)
           end))
      ' "$external_json" >/dev/null || fail "required external material key is absent"
  done < <(jq -r '.external_bindings[] | .kind as $kind | .name as $name |
    .keys[] | [$kind,$name,.] | @tsv' "$policy")
  while IFS=$'\t' read -r kind name expected_keys; do
    actual_keys=$(KIND="$kind" NAME="$name" jq -Sc '
      .[] | select(.kind == env.KIND and .metadata.name == env.NAME) |
      [((.data // {}) | keys[]), ((.binaryData // {}) | keys[])] | unique | sort
    ' "$external_json")
    [[ "$actual_keys" == "$expected_keys" ]] ||
      fail "external material key set mismatch"
  done < <(jq -r '.external_bindings[] |
    [.kind,.name,(.keys | unique | sort | tojson)] | @tsv' "$policy")
fi

if [[ -n "$expected_context" ]]; then
  while IFS=$'\t' read -r namespace kind name source_keys; do
    [[ "$kind" == Secret ]] || fail "unsupported reusable source kind"
    kubectl --context "$expected_context" -n "$namespace" get secret "$name" -o json |
      jq -e --argjson keys "$source_keys" '
        . as $secret |
        $secret.data != null and all($keys[]; . as $key |
          ($secret.data[$key] | type == "string" and length > 0))
      ' >/dev/null || fail "reusable source binding is absent or incomplete"
  done < <(jq -r '.reusable_bindings[] |
    [.source_namespace,.source_kind,.source_name,
     ([.key_map[]] | unique | tojson)] | @tsv' "$policy")
fi

output_directory=$(dirname -- "$output")
[[ -d "$output_directory" ]] || fail "output directory is absent"
output_name=$(basename -- "$output")
[[ "$output_name" != . && "$output_name" != .. && "$output_name" != / ]] ||
  fail "output path is invalid"
output_temporary=$(mktemp "$output_directory/.${output_name}.XXXXXX")
jq -S --slurpfile resources "$resources" '
  {
    schema_version,
    profile,
    namespace,
    prototype_secret_backend,
    resources:$resources[0],
    counts:($resources[0] | group_by(.classification) |
      map({key:.[0].classification,value:length}) | from_entries),
    external_bindings,
    reusable_bindings,
    runtime_owned_empty_resources,
    publisher_owned_empty_resources,
    reconciler_owned_empty_resources,
    publisher_owned_runtime_keys
  }
' "$policy" >"$output_temporary"
chmod 0600 "$output_temporary"
mv -f -- "$output_temporary" "$output"
output_temporary=""
printf 'Application material classification created: %s\n' "$output"
