#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Integration synthetic fixture test failed: %s\n' "$*" >&2
  exit 1
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
for command_name in go jq kubectl yq; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done

(
  cd -- "$repository_root/services/external/integration-gateway"
  env -u GOFLAGS GOENV=off GOWORK=off go test -count=1 -race \
    ./internal/integrationfixture ./internal/integration
)
"$repository_root/scripts/tests/integration-synthetic-fixture-e2e.sh"

temporary_directory=$(mktemp -d)
trap 'rm -rf "$temporary_directory"' EXIT
local_render="$temporary_directory/integration-synthetic.yaml"
env KUBECONFIG=/dev/null kubectl kustomize "$repository_root/deploy/k8s/overlays/local/integration-synthetic" >"$local_render"
local_json="$temporary_directory/integration-synthetic.json"
yq -o=json -I=0 '.' "$local_render" | jq -s 'map(select(.kind != null))' >"$local_json"

definition_json="$temporary_directory/synthetic-definition.json"
yq -o=json -I=0 '.' "$repository_root/contracts/integrations/v1/definitions/synthetic.yaml" >"$definition_json"
jq -e '
  .metadata.version == "3.0.0" and
  (.spec.healthCheck.operation == "synthetic.journal.read") and
  any(.spec.capabilities[];
    .key == "synthetic.journal.write" and
    .risk == "WRITE" and
    .approvalPolicy == "HUMAN_EACH_EFFECT" and
    .execution.idempotency == "PROVIDER_NATIVE" and
    .execution.maxAttempts == 3 and
    any(.inputFields[];
      .key == "action" and .allowedValues == ["CREATE", "UPDATE", "DELETE"]) and
    any(.inputFields[];
      .key == "fault" and .allowedValues == ["NONE", "RETRYABLE_ONCE", "TERMINAL"]))
' "$definition_json" >/dev/null || fail 'synthetic CRUD, Human Gate or fault contract is invalid'

jq -e '
  any(.[];
    .kind == "Deployment" and .metadata.name == "integration-synthetic" and
    .metadata.namespace == "kodex-system" and
    .metadata.labels["kodex.dev/local-only"] == "true" and
    .spec.replicas == 1 and .spec.strategy.type == "Recreate" and
    .spec.template.metadata.annotations["kodex.dev/fixture-contract-version"] == "3" and
    .spec.template.spec.automountServiceAccountToken == false and
    .spec.template.spec.securityContext.runAsNonRoot == true and
    .spec.template.spec.securityContext.seccompProfile.type == "RuntimeDefault" and
    any(.spec.template.spec.containers[];
      .name == "integration-synthetic" and
      .securityContext.allowPrivilegeEscalation == false and
      .securityContext.readOnlyRootFilesystem == true and
      .securityContext.capabilities.drop == ["ALL"] and
      .resources.requests.cpu == "25m" and .resources.requests.memory == "64Mi" and
      .resources.limits.cpu == "500m" and .resources.limits.memory == "512Mi"))
' "$local_json" >/dev/null || fail 'deployment security and resources are invalid'
jq -e '
  any(.[];
    .kind == "NetworkPolicy" and .metadata.name == "integration-synthetic-default-deny" and
    .spec.policyTypes == ["Ingress","Egress"] and
    (.spec | has("ingress") | not) and (.spec | has("egress") | not))
' "$local_json" >/dev/null || fail 'default-deny NetworkPolicy is invalid'
jq -e '
  any(.[];
    .kind == "NetworkPolicy" and .metadata.name == "integration-synthetic-exact-runtime-paths" and
    .spec.policyTypes == ["Ingress","Egress"] and .spec.egress == [] and
    .spec.ingress[0].from[0].namespaceSelector.matchLabels["kubernetes.io/metadata.name"] == "kodex-system" and
    .spec.ingress[0].from[0].podSelector.matchLabels["app.kubernetes.io/name"] == "integration-gateway" and
    .spec.ingress[0].from[0].podSelector.matchLabels["app.kubernetes.io/component"] == "integration-worker" and
    .spec.ingress[0].ports == [{"protocol":"TCP","port":8080}])
' "$local_json" >/dev/null || fail 'exact NetworkPolicy is invalid'

for profile in web-only web-with-mattermost; do
  profile_render="$temporary_directory/$profile.yaml"
  env KUBECONFIG=/dev/null kubectl kustomize "$repository_root/deploy/k8s/profiles/$profile" >"$profile_render"
  if yq -o=json -I=0 '.' "$profile_render" | jq -s -e 'any(.[]; .kind == "Deployment" and .metadata.name == "integration-synthetic")' >/dev/null; then
    fail "integration-synthetic leaked into $profile"
  fi
done

printf 'Integration synthetic fixture tests passed\n'
