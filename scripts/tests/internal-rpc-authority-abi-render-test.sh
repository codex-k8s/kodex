#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Internal RPC authority ABI render test failed: %s\n' "$*" >&2
  exit 1
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
render="$temporary_directory/render.yaml"
mutated="$temporary_directory/mutated.yaml"
abi=$(jq -er '.policy.authority_abi_version | tostring' \
  "$repository_root/deploy/k8s/base/internal-rpc-authority-publisher/authority-policy.json")

for profile in web-only web-with-mattermost; do
kubectl kustomize "$repository_root/deploy/k8s/profiles/$profile" >"$render"

validate_render() {
  yq -o=json -I=0 '.' "$1" | jq -s -e --arg abi "$abi" '
    [map(select(.kind == "Deployment"))[] |
      select(any((.spec.template.spec.containers[]?, .spec.template.spec.initContainers[]?);
        (.name | test("(^internal-rpc-authority-(issuer|verifier)$|platform-worker-grant-agent$)"))))] as $deployments |
    ($deployments | length) > 0 and
    all($deployments[];
      .spec.template.metadata.labels."kodex.dev/internal-rpc-authority-abi" == $abi and
      ([ (.spec.template.spec.containers[]?, .spec.template.spec.initContainers[]?) |
        select(.name | test("(^internal-rpc-authority-(issuer|verifier)$|platform-worker-grant-agent$)")) |
        .image] | unique | length) == 1)
  ' >/dev/null
}

validate_render "$render" || fail 'active workloads do not share the declared authority ABI and image'

# Общая компонента не назначает identity: каждый workload сохраняет точные
# workload/SPIFFE/PostgreSQL pins. Проверяем результат после всех overlays.
validate_issuer_identities() {
  yq -o=json -I=0 '.' "$1" | jq -s -e '
    def exact_env($key; $expected):
      [.env[]? | select(.name == $key)] as $entries |
      ($entries | length) == 1 and $entries[0] == $expected;
    [ .[] | select(.kind == "Deployment") |
      . as $deployment |
      (.spec.template.spec.containers[]?, .spec.template.spec.initContainers[]?) |
      select(.name == "internal-rpc-authority-issuer") |
      {deployment: $deployment, issuer: .} ] as $issuers |
    ($issuers | length) > 0 and
    all($issuers[];
      .deployment.metadata.name as $workload |
      .deployment.metadata.namespace as $namespace |
      .issuer |
      exact_env("INTERNAL_RPC_AUTHORITY_WORKLOAD_ID";
        {name: "INTERNAL_RPC_AUTHORITY_WORKLOAD_ID", value: $workload}) and
      exact_env("INTERNAL_RPC_AUTHORITY_WORKLOAD_SPIFFE_ID";
        {name: "INTERNAL_RPC_AUTHORITY_WORKLOAD_SPIFFE_ID",
         value: ("spiffe://kodex.local/ns/" + $namespace + "/sa/" + $workload)}) and
      exact_env("INTERNAL_RPC_AUTHORITY_POSTGRES_EXPECTED_SESSION_USER";
        {name: "INTERNAL_RPC_AUTHORITY_POSTGRES_EXPECTED_SESSION_USER",
         valueFrom: {secretKeyRef: {name: ("internal-rpc-authority-" + $workload + "-issuer-postgresql"), key: "username"}}}))
  ' >/dev/null
}
validate_issuer_identities "$render" || fail 'issuer workload identity pins are invalid'

# Каждая мутация независимо проверяет отсутствие подмены или дубликата identity.
for mutation in \
  '(.env[] | select(.name == "INTERNAL_RPC_AUTHORITY_WORKLOAD_ID")).value = "other-workload"' \
  '(.env[] | select(.name == "INTERNAL_RPC_AUTHORITY_WORKLOAD_SPIFFE_ID")).value = "spiffe://kodex.local/ns/kodex-system/sa/other-workload"' \
  '(.env[] | select(.name == "INTERNAL_RPC_AUTHORITY_POSTGRES_EXPECTED_SESSION_USER")).valueFrom.secretKeyRef.name = "other-issuer-postgresql"' \
  '.env += [{name: "INTERNAL_RPC_AUTHORITY_WORKLOAD_ID", value: "other-workload"}]'; do
  yq -o=json -I=0 '.' "$render" | jq -s \
    'map(select(.kind == "Deployment" and .metadata.name == "control-api-gateway"))[0]' |
    jq '(.spec.template.spec.containers[] | select(.name == "internal-rpc-authority-issuer")) |= ('"$mutation"')' >"$mutated"
  if validate_issuer_identities "$mutated"; then
    fail 'invalid issuer identity mutation was accepted'
  fi
done

validate_controller_sidecars() {
  yq -o=json -I=0 '.' "$1" | jq -s -e '
  any(.[];
    .kind == "Deployment" and .metadata.name == "runtime-controller" and
    ([.spec.template.spec.initContainers[].name] ==
      ["internal-rpc-authority-socket-init", "internal-rpc-authority-issuer", "platform-worker-grant-agent", "artifact-spool-init"]) and
    all(.spec.template.spec.initContainers[] | select(.name == "internal-rpc-authority-socket-init" or .name == "artifact-spool-init");
      .restartPolicy == null and .startupProbe == null and .readinessProbe == null and .livenessProbe == null) and
    all(.spec.template.spec.initContainers[] | select(.name == "internal-rpc-authority-issuer" or .name == "platform-worker-grant-agent");
      .restartPolicy == "Always" and .startupProbe.httpGet.path == "/readyz" and
      .readinessProbe.httpGet.path == "/readyz" and
      .resources.requests.cpu != null and .resources.requests.memory != null and
      .resources.limits.cpu != null and .resources.limits.memory != null) and
    ([.spec.template.spec.containers[].name] == ["runtime-controller"]))
  ' >/dev/null
}
validate_controller_sidecars "$render" || fail 'controller authority sidecar startup or termination ordering is invalid'

# Каждая мутация независимо проверяет закрытый набор, порядок и native sidecar policy.
for mutation in \
  '.spec.template.spec.initContainers |= reverse' \
  'del(.spec.template.spec.initContainers[3])' \
  '.spec.template.spec.initContainers += [{"name":"unknown-init"}]' \
  '.spec.template.spec.initContainers[3].name = "unknown-init"' \
  '.spec.template.spec.initContainers[3].restartPolicy = "Always"' \
  '.spec.template.spec.initContainers[0].restartPolicy = "Always"' \
  '.spec.template.spec.initContainers[2].restartPolicy = "Never"' \
  'del(.spec.template.spec.initContainers[1].startupProbe)' \
  'del(.spec.template.spec.initContainers[2].readinessProbe)' \
  '.spec.template.spec.initContainers[3].startupProbe.httpGet.path = "/readyz"'; do
  yq -o=json -I=0 '.' "$render" | jq -s \
    'map(select(.kind == "Deployment" and .metadata.name == "runtime-controller"))[0]' |
    jq "$mutation" >"$mutated"
  if validate_controller_sidecars "$mutated"; then
    fail 'invalid controller initializer mutation was accepted'
  fi
done

yq 'select(.kind == "Deployment" and .metadata.name == "runtime-controller") |
  del(.spec.template.spec.initContainers[] | select(.name == "internal-rpc-authority-issuer") | .restartPolicy)' \
  "$render" >"$mutated"
if validate_controller_sidecars "$mutated"; then
  fail 'controller issuer without native shutdown ordering was accepted'
fi

yq 'select(.kind == "Deployment" and .metadata.name == "runtime-controller") |
  .spec.template.spec.initContainers[] |=
    (select(.name == "internal-rpc-authority-issuer").image =
      "ghcr.io/codex-k8s/kodex/internal-rpc-authority@sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")' \
  "$render" >"$mutated"
if validate_render "$mutated"; then
  fail 'incompatible native sidecar authority image was accepted'
fi

yq 'select(.kind == "Deployment" and .metadata.name == "control-plane") |
  .spec.template.spec.containers[] |=
    (select(.name == "control-plane-platform-worker-grant-agent").image =
      "ghcr.io/codex-k8s/kodex/internal-rpc-authority@sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")' \
  "$render" >"$mutated"
if validate_render "$mutated"; then
  fail 'incompatible control-plane grant agent image was accepted'
fi

yq 'select(.kind == "Deployment" and .metadata.name == "secret-broker") |
  .spec.template.metadata.labels."kodex.dev/internal-rpc-authority-abi" = "1"' \
  "$render" >"$mutated"
if validate_render "$mutated"; then
  fail 'incompatible authority ABI label was accepted'
fi

done
printf 'Internal RPC authority ABI render test passed\n'
