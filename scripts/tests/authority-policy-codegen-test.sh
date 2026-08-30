#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Authority policy codegen test failed: %s\n' "$*" >&2
  exit 1
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
generated="$temporary_directory/authority-policy.json"
canonical="$repository_root/deploy/k8s/base/internal-rpc-authority-publisher/authority-policy.json"

(
  cd -- "$repository_root/libs/go/controlplaneclient"
  env -u GOFLAGS GOENV=off GOWORK=off go run ./cmd/policygen \
    --output "$generated" \
    --oidc-issuer '__KODEX_OIDC_ISSUER__' \
    --oidc-audience kodex-control-api
)

cmp -s "$generated" "$canonical" || fail 'generated policy differs from the canonical file'
jq -e '
  def provider_operations: [
    "platform.provider-credentials.api-key.materialize",
    "platform.provider-credentials.device-authorize.get",
    "platform.provider-credentials.device-authorize.start",
    "platform.provider-credentials.materialization.discard",
    "platform.provider-credentials.readiness.check"
  ];
  .v == 1 and .policy.default_decision == "DENY" and
  .policy_revision == 41 and
  (.policy.authority_proof_producers | length) == 11 and
  ((.policy.operation_bindings | map(.operation_id) | unique | length) ==
   (.policy.operation_bindings | length)) and
  all(.policy.operation_bindings[];
    .permission != "" and .full_method != "" and
    .authority_proof_producer_id != "") and
  ([.policy.operation_bindings[] |
    select(.target_workload_id == "secret-broker") | .operation_id] | sort) == provider_operations and
  all(.policy.operation_bindings[] | select(.target_workload_id == "secret-broker");
    .caller_workload_id == "control-plane" and
    .caller_spiffe_id == "spiffe://kodex.local/ns/kodex-system/sa/control-plane" and
    .target_spiffe_id == "spiffe://kodex.local/ns/kodex-system/sa/secret-broker" and
    .audience == "urn:kodex:internal-rpc:secret-broker" and
    .target_tls_server_name == "secret-broker.kodex-system.svc.cluster.local" and
    .authority_proof_producer_id == "secret-broker.provider-credential-materializer") and
  all(.policy.operation_bindings[] | select(.target_workload_id != "secret-broker");
    .target_workload_id == "control-plane") and
  ([.policy.authority_proof_producers[] |
    select(.producer_id == "secret-broker.provider-credential-materializer")] | length) == 1 and
  all(.policy.authority_proof_producers[] |
    select(.producer_id == "secret-broker.provider-credential-materializer");
    .caller_workload_id == "control-plane" and
    .owner_workload_id == "control-plane" and
    .application_credential == "PLATFORM_WORKER_GRANT" and
    (.allowed_operation_ids | sort) == provider_operations)
' "$canonical" >/dev/null || fail 'canonical policy invariants are invalid'

printf 'Authority policy codegen tests passed\n'
