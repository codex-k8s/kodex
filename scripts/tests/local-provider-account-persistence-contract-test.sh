#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex local provider account persistence contract test failed: %s\n' "$*" >&2
  exit 1
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
provider_script="$repository_root/tools/dev/provider-account.sh"
reconcile_sql="$repository_root/tools/dev/reconcile-provider-account.sql"
dev_script="$repository_root/dev.sh"

grep -Fq 'canonical_auth_file="$account_home/auth.json"' "$provider_script" ||
  fail 'provider import does not retain a canonical private auth snapshot'
grep -Fq '{version:1, accountKey:$account_key, name:$account_name, authorizationMode:$authorization_mode}' "$provider_script" ||
  fail 'provider account metadata is not persisted'
grep -Fq 'control_namespace=kodex-system' "$provider_script" ||
  fail 'provider metadata does not declare the control namespace'
grep -Fq 'runtime_namespace=kodex-runtime' "$provider_script" ||
  fail 'provider credential does not declare the runtime namespace'
grep -Fq 'kubectl -n "$runtime_namespace" create secret generic "$secret_name"' "$provider_script" ||
  fail 'provider credential is not materialized in the runtime namespace'
[[ $(grep -Fc 'kubectl -n "$runtime_namespace" get "secret/$secret_name"' "$provider_script") -eq 3 ]] ||
  fail 'provider credential identity is not read back from the runtime namespace'
if grep -Fq 'kubectl -n "$control_namespace" create secret generic "$secret_name"' "$provider_script"; then
  fail 'provider credential is still materialized in the control namespace'
fi
grep -Fq 'legacy provider Secret in control namespace is not owned by local development' "$provider_script" ||
  fail 'legacy provider credential cleanup is not ownership guarded'
grep -Fq 'default_provider_auth="$state_directory/provider-accounts/default-openai-codex/auth.json"' "$dev_script" ||
  fail 'default provider authorization is not isolated in its account directory'
grep -Fq 'provider_auth=${KODEX_DEV_PROVIDER_AUTH_FILE:-$default_provider_auth}' "$dev_script" ||
  fail 'immutable installation material is not separated from provider account revisions'
grep -Fq 'provider_metadata=("$state_directory"/provider-accounts/*/account.json)' "$dev_script" ||
  fail 'local deployment does not discover persisted provider accounts'
grep -Fq 'provider account metadata directory binding is invalid' "$dev_script" ||
  fail 'provider metadata is not bound to its account directory'
grep -Fq 'restored_provider_accounts > 0' "$dev_script" ||
  fail 'runtime readiness is not rechecked after provider reconciliation'
grep -Fq 'ON CONFLICT (ref) DO UPDATE' "$reconcile_sql" ||
  fail 'provider reconciliation does not use the current immutable account identity'
if grep -Fq 'ON CONFLICT (organization_id, stable_key)' "$reconcile_sql"; then
  fail 'provider reconciliation relies on the removed stable-key uniqueness constraint'
fi
[[ $(grep -Fc "WHERE account.ref = :'account_ref'" "$reconcile_sql") -ge 4 ]] ||
  fail 'provider credential reconciliation is not bound to the exact account ref'

printf 'Kodex local provider account persistence contract test passed\n'
