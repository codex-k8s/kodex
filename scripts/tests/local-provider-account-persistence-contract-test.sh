#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex local provider account persistence contract test failed: %s\n' "$*" >&2
  exit 1
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
provider_script="$repository_root/tools/dev/provider-account.sh"
dev_script="$repository_root/dev.sh"

grep -Fq 'canonical_auth_file="$account_home/auth.json"' "$provider_script" ||
  fail 'provider import does not retain a canonical private auth snapshot'
grep -Fq '{version:1, accountKey:$account_key, name:$account_name}' "$provider_script" ||
  fail 'provider account metadata is not persisted'
grep -Fq 'provider_auth=${KODEX_DEV_PROVIDER_AUTH_FILE:-"$state_directory/inputs/openai-auth.json"}' "$dev_script" ||
  fail 'immutable installation material is not separated from provider account revisions'
grep -Fq 'provider_metadata=("$state_directory"/provider-accounts/*/account.json)' "$dev_script" ||
  fail 'local deployment does not discover persisted provider accounts'
grep -Fq 'provider account metadata directory binding is invalid' "$dev_script" ||
  fail 'provider metadata is not bound to its account directory'
grep -Fq 'restored_provider_accounts > 0' "$dev_script" ||
  fail 'runtime readiness is not rechecked after provider reconciliation'

printf 'Kodex local provider account persistence contract test passed\n'
