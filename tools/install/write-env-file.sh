#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex env file generation failed: %s\n' "$*" >&2
  exit 1
}

output=""
while (($# > 0)); do
  case "$1" in
    --output) output="${2:-}"; shift 2 ;;
    --help)
      printf 'Usage: %s --output <new-.kodex-env-path>\n' "$0" >&2
      exit 0
      ;;
    *) fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$output" && "$output" != / && ! -e "$output" ]] ||
  fail 'new output path is required'
output_directory=$(dirname -- "$output")
[[ -d "$output_directory" && -w "$output_directory" ]] ||
  fail 'output directory is unavailable'

keys=(
  KODEX_INSTALL_MODE KODEX_NAMESPACE KODEX_KUBECONFIG KODEX_KUBE_CONTEXT
  KODEX_SERVER_PUBLIC_IP KODEX_PUBLIC_IPV4_CIDR
  KODEX_CONTROL_HOST KODEX_CONTROL_TLS_RECOVERY_HOST
  KODEX_OIDC_HOST KODEX_OIDC_CONNECT_ADDRESS
  KODEX_OIDC_TLS_SERVER_NAME KODEX_OIDC_NAMESPACE KODEX_OIDC_POD_NAME
  KODEX_OIDC_POD_COMPONENT KODEX_OIDC_TARGET_PORT KODEX_GRAFANA_HOST
  KODEX_HEADLAMP_HOST KODEX_REGISTRY_HOST KODEX_PROMOTED_PULL_HOST
  KODEX_INGRESS_CLASS KODEX_CLUSTER_ISSUER KODEX_ACME_EMAIL KODEX_ACME_SERVER
  KODEX_INGRESS_NAMESPACE KODEX_INGRESS_POD_NAME KODEX_INGRESS_SERVICE_NAME
  KODEX_KEYCLOAK_ADMIN_USERNAME KODEX_KEYCLOAK_ADMIN_INITIAL_PASSWORD
  KODEX_OWNER_USERNAME KODEX_OWNER_EMAIL KODEX_OWNER_INITIAL_PASSWORD
  KODEX_GITHUB_ARC_TOKEN KODEX_GITHUB_OWNER_PAT
  KODEX_RELEASE_REGISTRY_USERNAME KODEX_RELEASE_REGISTRY_PASSWORD
  KODEX_OPENAI_AUTH_JSON_B64 KODEX_OPENAI_AUTH_JSON_FILE
  KODEX_SENTRY_DSN KODEX_DISABLE_OBSERVABILITY KODEX_ENABLE_EXTERNAL_S3
  KODEX_S3_ENDPOINT KODEX_S3_REGION KODEX_S3_BUCKET KODEX_S3_ACCESS_KEY
  KODEX_S3_SECRET_KEY KODEX_MATERIAL_DIRECTORY
)

umask 077
temporary_file=$(mktemp "$output_directory/.kodex-env.XXXXXX")
cleanup() { rm -f -- "$temporary_file"; }
trap cleanup EXIT
for key in "${keys[@]}"; do
  value=${!key:-}
  [[ -n "$value" ]] || continue
  [[ "$value" != *$'\n'* && "$value" != *$'\r'* && "$value" != *'`'* &&
    "$value" != *'$('* ]] || fail "unsafe value for $key"
  printf '%s=%s\n' "$key" "$value" >>"$temporary_file"
done
[[ -s "$temporary_file" ]] || fail 'no KODEX variables are set'
chmod 0600 "$temporary_file"
mv -- "$temporary_file" "$output"
trap - EXIT
printf 'Kodex env file generated: %s\n' "$output"
