#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'agent-runner test failed: %s\n' "$*" >&2
  exit 1
}

script_directory="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
repository_root="$(cd -- "$script_directory/../.." && pwd -P)"

for command_name in go jq kubectl yq bwrap; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done

(
  cd "$repository_root/libs/go/runtimecontract"
  env -u GOFLAGS GOENV=off GOWORK=off go test -count=1 ./...
)
(
  cd "$repository_root/services/jobs/agent-runner"
  env -u GOFLAGS GOENV=off GOWORK=off go test -count=1 ./...
)

schema="$repository_root/contracts/runtime-controller/v7/agent-runner-input.schema.json"
jq -e '
  .additionalProperties == false and
  .properties.schema.const == "kodex.agent-runner-input.v7" and
  (["runtime_revision_ref", "runtime_revision_version", "runtime_revision_digest",
    "instruction_ref", "instruction_digest", "prompt_template_ref",
    "prompt_template_digest", "prompt_materialization_digest", "workspace_policy",
    "effective_reasoning_effort", "reasoning_mode"] - .required | length == 0) and
  (.properties.reasoning_mode.enum == ["SUPPORTED", "UNSUPPORTED"]) and
  (.properties.execution_binding_digest."$ref" == "#/$defs/sha256") and
  (.properties.mcp_binding_digest."$ref" == "#/$defs/sha256") and
  (."$defs".sha256.pattern == "^[a-f0-9]{64}$")
' "$schema" >/dev/null || fail 'immutable runner schema is incomplete'

render="$(mktemp)"
trap 'rm -f -- "$render"' EXIT
kubectl kustomize "$repository_root/deploy/k8s/base/runtime-workloads" >"$render" ||
  fail 'runtime workload render failed'

yq -o=json -I=0 '.' "$render" | jq -s -e '
  . as $resources |
  any($resources[];
    .kind == "ServiceAccount" and .metadata.name == "agent-runner" and
    .metadata.namespace == "kodex-runtime" and .automountServiceAccountToken == false) and
  any($resources[];
    .kind == "NetworkPolicy" and .metadata.name == "runtime-controller-agent-runner-default-deny" and
    .metadata.namespace == "kodex-runtime" and .spec.podSelector == {} and
    ((.spec.policyTypes | sort) == (["Egress", "Ingress"] | sort)) and
    (.spec.ingress == null) and (.spec.egress == null)) and
  any($resources[];
    .kind == "NetworkPolicy" and .metadata.name == "runtime-controller-warm-runner-exact-paths" and
    .metadata.namespace == "kodex-runtime" and
    .spec.podSelector.matchLabels."app.kubernetes.io/name" == "agent-runner" and
    .spec.podSelector.matchLabels."runtime.kodex.dev/mode" == "warm" and
    .spec.ingress == [] and (.spec.egress | length) == 3 and
    any(.spec.egress[].to[]?; .podSelector.matchLabels."app.kubernetes.io/name" == "runtime-controller") and
    any(.spec.egress[].to[]?; .podSelector.matchLabels."app.kubernetes.io/name" == "egress-gateway")) and
  (any($resources[];
    (.kind == "Role" or .kind == "RoleBinding") and
    (.metadata.name | test("agent-runner"))) | not)
' >/dev/null || fail 'agent-runner ServiceAccount, RBAC or NetworkPolicy boundary is incomplete'

printf 'agent-runner tests passed\n'
