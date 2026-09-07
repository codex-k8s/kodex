#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex local provider account failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    'Usage:' \
    '  provider-account.sh authorize --account-key <key> --name <name> --mode device|api-key' \
    '  provider-account.sh import --account-key <key> --name <name> --auth-file <path>' \
    '  provider-account.sh list' \
    'Options: --kubeconfig <path> --context <name> --state-directory <path>' >&2
}

command_name=${1:-}
[[ -n "$command_name" ]] || { usage; exit 1; }
shift

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
kubeconfig=${KODEX_DEV_KUBECONFIG:-"$HOME/.kube/kodex-dev-local"}
context=${KODEX_DEV_KUBE_CONTEXT:-default}
state_directory="$repository_root/.kodex-dev"
account_key=""
account_name=""
auth_mode=""
auth_file=""
preserve_current=()
while (($# > 0)); do
  case "$1" in
    --account-key) account_key=${2:-}; shift 2 ;;
    --name) account_name=${2:-}; shift 2 ;;
    --mode) auth_mode=${2:-}; shift 2 ;;
    --auth-file) auth_file=${2:-}; shift 2 ;;
    --preserve-current) preserve_current=(--preserve-current); shift ;;
    --kubeconfig) kubeconfig=${2:-}; shift 2 ;;
    --context) context=${2:-}; shift 2 ;;
    --state-directory) state_directory=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

case "$command_name" in authorize|import|list) ;; *) usage; fail 'command is invalid' ;; esac
for command in codex install jq kubectl mktemp realpath sha256sum stat python3; do
  command -v "$command" >/dev/null 2>&1 || fail "$command is required"
done
[[ -f "$kubeconfig" && -r "$kubeconfig" ]] || fail 'Kubernetes configuration is absent'
[[ "$state_directory" == /* && "$state_directory" != / && "$state_directory" != "$HOME" ]] ||
  fail 'state directory must be an exact safe absolute path'
export KUBECONFIG=$kubeconfig
[[ "$(kubectl config current-context)" == "$context" ]] || fail 'Kubernetes context mismatch'
[[ "$context" != *prod* && "$context" != *production* ]] || fail 'production context is forbidden'
kubectl get --raw=/readyz >/dev/null || fail 'Kubernetes API is unavailable'
control_namespace=kodex-system
runtime_namespace=kodex-runtime
for namespace_name in "$control_namespace" "$runtime_namespace"; do
  kubectl get "namespace/$namespace_name" >/dev/null 2>&1 ||
    fail "required Kubernetes namespace is absent: $namespace_name"
done

if [[ "$command_name" == list ]]; then
  kubectl -n "$control_namespace" exec kodex-postgresql-0 -- psql -U postgres -d control_plane \
    -P pager=off -c "SELECT account.stable_key, account.name, account.state, account.enabled, revision.revision_number FROM control_plane.provider_accounts account LEFT JOIN control_plane.provider_credential_revisions revision ON revision.id = account.current_credential_revision_id ORDER BY account.created_at"
  exit 0
fi

[[ "$account_key" =~ ^[a-z][a-z0-9_-]{1,95}$ ]] || fail 'account key is invalid'
[[ -n "$account_name" && ${#account_name} -le 160 && "$account_name" != *$'\n'* && "$account_name" != *$'\r'* ]] ||
  fail 'account name is invalid'

account_home="$state_directory/provider-accounts/$account_key"
if [[ "$command_name" == authorize ]]; then
  case "$auth_mode" in device|api-key) ;; *) fail 'authorization mode is invalid' ;; esac
  install -d -m 0700 "$account_home"
  if [[ "$auth_mode" == device ]]; then
    CODEX_HOME="$account_home" codex login --device-auth
  else
    [[ ! -t 0 ]] || fail 'API key must be provided through standard input'
    CODEX_HOME="$account_home" codex login --with-api-key
  fi
  auth_file="$account_home/auth.json"
fi

[[ "$auth_file" == /* && -f "$auth_file" && ! -L "$auth_file" ]] ||
  fail 'authorization file must be an absolute regular non-symlink file'
[[ "$(stat -c '%u' "$auth_file")" == "$(id -u)" ]] || fail 'authorization file owner is invalid'
permissions=$(stat -c '%a' "$auth_file")
(( (8#$permissions & 8#077) == 0 )) || fail 'authorization file must be private'
[[ "$(stat -c '%s' "$auth_file")" -le 1048576 ]] || fail 'authorization file is too large'
jq -e 'type == "object" and length > 0' "$auth_file" >/dev/null || fail 'authorization JSON is invalid'
CODEX_HOME="$(dirname -- "$auth_file")" codex login status >/dev/null ||
  fail 'Codex does not recognize the authorization file'
stored_auth_mode=$(jq -r '.auth_mode // ""' "$auth_file")
case "$stored_auth_mode" in
  chatgpt)
    authorization_mode=managed-chatgpt-oauth
    max_concurrent_executions=1
    ;;
  apikey|api-key)
    authorization_mode=api-key
    max_concurrent_executions=32
    ;;
  *) fail 'Codex authorization mode is unsupported' ;;
esac

install -d -m 0700 "$account_home"
canonical_auth_file="$account_home/auth.json"
if [[ "$(realpath -e -- "$auth_file")" != "$canonical_auth_file" ]]; then
  temporary_auth_file=$(mktemp "$account_home/.auth.json.XXXXXX")
  install -m 0600 "$auth_file" "$temporary_auth_file"
  mv -f -- "$temporary_auth_file" "$canonical_auth_file"
fi
auth_file="$canonical_auth_file"

key_digest=$(printf '%s' "$account_key" | sha256sum | awk '{print $1}')
account_ref="pacc_${key_digest:0:24}"
if [[ "$account_key" != default-openai-codex ]]; then
  kubectl -n "$control_namespace" exec -i kodex-postgresql-0 -- \
    psql -XqAt -U postgres -d control_plane -v ON_ERROR_STOP=1 \
    -v account_ref="$account_ref" -v stable_key="$account_key" -v account_name="$account_name" \
    -v max_concurrent_executions="$max_concurrent_executions" \
    <"$repository_root/tools/dev/provider-account-ensure.sql" >/dev/null 2>&1 ||
    fail 'provider account owner reservation failed'
fi
python3 "$repository_root/tools/install/provider-bootstrap.py" import \
  --context "$context" --account-key "$account_key" --auth-file "$auth_file" "${preserve_current[@]}"

metadata_file="$account_home/account.json"
temporary_metadata=$(mktemp "$account_home/.account.json.XXXXXX")
jq -n --arg account_key "$account_key" --arg account_name "$account_name" --arg authorization_mode "$authorization_mode" '
  {version:1, accountKey:$account_key, name:$account_name, authorizationMode:$authorization_mode}
' >"$temporary_metadata"
chmod 0600 "$temporary_metadata"
mv -f -- "$temporary_metadata" "$metadata_file"

printf 'Kodex provider account reconciled: %s\n' "$account_key"
