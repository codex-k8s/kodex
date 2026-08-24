#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'Vault OIDC configuration failed: %s\n' "$*" >&2; exit 1; }
usage() {
  printf '%s\n' \
    "Usage: $0 --context <exact-context> --mode apply|readback" \
    '  --material-directory <owner-material-directory> --oidc-issuer <https-url>' \
    '  --vault-public-origin <https-origin>' >&2
}

context=""
mode=""
material_directory=""
oidc_issuer=""
vault_public_origin=""
while (($# > 0)); do
  case "$1" in
    --context) context="${2:-}"; shift 2 ;;
    --mode) mode="${2:-}"; shift 2 ;;
    --material-directory) material_directory="${2:-}"; shift 2 ;;
    --oidc-issuer) oidc_issuer="${2:-}"; shift 2 ;;
    --vault-public-origin) vault_public_origin="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$context" ]] || fail 'exact context is required'
case "$mode" in apply|readback) ;; *) fail 'mode is invalid' ;; esac
[[ -d "$material_directory/vault" && ! -L "$material_directory" ]] || fail 'owner material directory is invalid'
[[ "$oidc_issuer" =~ ^https://[a-zA-Z0-9._:-]+/realms/[a-zA-Z0-9._-]+$ ]] || fail 'OIDC issuer is invalid'
[[ "$vault_public_origin" =~ ^https://[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] || fail 'Vault public origin is invalid'
for command_name in base64 jq kubectl stat; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ "$(kubectl config current-context)" == "$context" ]] || fail 'current Kubernetes context mismatch'
root_token_file="$material_directory/vault/root-token"
[[ -f "$root_token_file" && -s "$root_token_file" && ! -L "$root_token_file" ]] || fail 'Vault root token material is absent'
(((8#$(stat -c '%a' "$root_token_file") & 0077) == 0)) || fail 'Vault root token permissions are too broad'
kubectl -n mattercodex-system get pod vault-0 >/dev/null 2>&1 || fail 'Vault Pod is absent'

temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
kubectl -n platform-admin get secret vault-oidc -o json >"$temporary_directory/vault-oidc.json" || fail 'Vault OIDC Secret is absent'
client_id=$(jq -er '.data["client-id"] | @base64d' "$temporary_directory/vault-oidc.json")
client_secret=$(jq -er '.data["client-secret"] | @base64d' "$temporary_directory/vault-oidc.json")
[[ "$client_id" == mattercodex-vault-ui && ${#client_secret} -ge 32 ]] || fail 'Vault OIDC Secret is invalid'
root_token=$(<"$root_token_file")

vault_command() {
  printf '%s\n' "$root_token" | kubectl -n mattercodex-system exec -i vault-0 -- sh -ec '
    IFS= read -r VAULT_TOKEN
    export VAULT_TOKEN VAULT_ADDR=https://vault.mattercodex-system.svc.cluster.local:8200
    export VAULT_CACERT=/vault/userconfig/vault-server-tls/ca.crt
    exec vault "$@"
  ' sh "$@"
}

vault_json() {
  local body=$1
  shift
  { printf '%s\n' "$root_token"; cat "$body"; } |
    kubectl -n mattercodex-system exec -i vault-0 -- sh -ec '
      IFS= read -r VAULT_TOKEN
      export VAULT_TOKEN VAULT_ADDR=https://vault.mattercodex-system.svc.cluster.local:8200
      export VAULT_CACERT=/vault/userconfig/vault-server-tls/ca.crt
      exec vault "$@"
    ' sh "$@"
}

if [[ "$mode" == apply ]]; then
  vault_command auth list -format=json | jq -e 'has("oidc/")' >/dev/null || vault_command auth enable -path=oidc oidc >/dev/null
  jq -n --arg issuer "$oidc_issuer" --arg client_id "$client_id" --arg client_secret "$client_secret" '{
    oidc_discovery_url:$issuer,
    oidc_client_id:$client_id,
    oidc_client_secret:$client_secret,
    default_role:"mattercodex-owner"
  }' >"$temporary_directory/config.json"
  vault_json "$temporary_directory/config.json" write auth/oidc/config - >/dev/null
  printf '%s\n' 'path "*" { capabilities = ["create", "read", "update", "patch", "delete", "list", "sudo"] }' \
    >"$temporary_directory/owner-policy.hcl"
  vault_json "$temporary_directory/owner-policy.hcl" policy write mattercodex-owner - >/dev/null
  jq -n --arg audience "$client_id" \
    --arg callback "$vault_public_origin/ui/vault/auth/oidc/oidc/callback" '{
      role_type:"oidc",
      user_claim:"sub",
      groups_claim:"/realm_access/roles",
      bound_audiences:[$audience],
      bound_claims:{"/realm_access/roles":"mattercodex-owner"},
      allowed_redirect_uris:[$callback],
      oidc_scopes:["openid","profile","email"],
      token_policies:["mattercodex-owner"],
      token_ttl:3600,
      token_max_ttl:28800
    }' >"$temporary_directory/role.json"
  vault_json "$temporary_directory/role.json" write auth/oidc/role/mattercodex-owner - >/dev/null
fi

vault_command read -format=json auth/oidc/config | jq -e --arg issuer "$oidc_issuer" --arg client_id "$client_id" '
  .data.oidc_discovery_url == $issuer and .data.oidc_client_id == $client_id and
  .data.default_role == "mattercodex-owner"
' >/dev/null || fail 'Vault OIDC config readback failed'
vault_command read -format=json auth/oidc/role/mattercodex-owner | jq -e \
  --arg callback "$vault_public_origin/ui/vault/auth/oidc/oidc/callback" '
    .data.role_type == "oidc" and .data.allowed_redirect_uris == [$callback] and
    (.data.token_policies | index("mattercodex-owner") != null)
  ' >/dev/null || fail 'Vault OIDC role readback failed'
unset client_secret root_token
printf 'Vault OIDC configuration completed: %s\n' "$mode"
