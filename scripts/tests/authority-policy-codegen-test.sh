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
    --oidc-issuer '__MATTERCODEX_OIDC_ISSUER__' \
    --oidc-audience mattercodex-control-api
)

cmp -s "$generated" "$canonical" || fail 'generated policy differs from the canonical file'
jq -e '
  .v == 1 and .policy.default_decision == "DENY" and
  (.policy.authority_proof_producers | length) == 7 and
  ((.policy.operation_bindings | map(.operation_id) | unique | length) ==
   (.policy.operation_bindings | length)) and
  all(.policy.operation_bindings[];
    .permission == .operation_id and .full_method != "" and
    .target_workload_id == "control-plane" and
    .authority_proof_producer_id != "")
' "$canonical" >/dev/null || fail 'canonical policy invariants are invalid'

printf 'Authority policy codegen tests passed\n'
