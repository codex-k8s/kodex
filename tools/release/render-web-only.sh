#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Web-only render failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 --lock <release-lock.json> --lock-sha256 <64-hex> --output <render.yaml>" \
    '  --public-host <dns> --public-origin <https://dns>' \
    '  --oidc-issuer <https-url> --oidc-jwks-url <https-url>' \
    '  --oidc-connect-address <host:port> --oidc-tls-server-name <dns>' >&2
}

lock_file=""
lock_sha256=""
output=""
public_host=""
public_origin=""
oidc_issuer=""
oidc_jwks_url=""
oidc_connect_address=""
oidc_tls_server_name=""

while (($# > 0)); do
  case "$1" in
    --lock) lock_file="${2:-}"; shift 2 ;;
    --lock-sha256) lock_sha256="${2:-}"; shift 2 ;;
    --output) output="${2:-}"; shift 2 ;;
    --public-host) public_host="${2:-}"; shift 2 ;;
    --public-origin) public_origin="${2:-}"; shift 2 ;;
    --oidc-issuer) oidc_issuer="${2:-}"; shift 2 ;;
    --oidc-jwks-url) oidc_jwks_url="${2:-}"; shift 2 ;;
    --oidc-connect-address) oidc_connect_address="${2:-}"; shift 2 ;;
    --oidc-tls-server-name) oidc_tls_server_name="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -r "$lock_file" ]] || fail 'release lock is not readable'
[[ "$lock_sha256" =~ ^[a-f0-9]{64}$ && "$lock_sha256" != 0000000000000000000000000000000000000000000000000000000000000000 ]] ||
  fail 'release lock SHA-256 is invalid'
[[ -n "$output" ]] || fail 'output path is required'
[[ "$public_host" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ && "$public_host" == *.* ]] || fail 'public host is invalid'
[[ "$public_origin" == "https://$public_host" ]] || fail 'public origin must be the exact HTTPS public host'
[[ "$oidc_issuer" =~ ^https://[a-zA-Z0-9._:-]+(/[^[:space:]]*)?$ ]] || fail 'OIDC issuer is invalid'
[[ "$oidc_jwks_url" =~ ^https://[a-zA-Z0-9._:-]+(/[^[:space:]]*)?$ ]] || fail 'OIDC JWKS URL is invalid'
[[ "$oidc_connect_address" =~ ^[a-zA-Z0-9._-]+:[1-9][0-9]{0,4}$ ]] || fail 'OIDC connect address is invalid'
[[ "$oidc_tls_server_name" =~ ^[a-zA-Z0-9]([a-zA-Z0-9.-]*[a-zA-Z0-9])?$ ]] ||
  fail 'OIDC TLS server name is invalid'

for command_name in kubectl yq jq sha256sum; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repository_root=$(cd -- "$script_directory/../.." && pwd -P)
source_sha=$(jq -er '.source_sha' "$lock_file")
"$script_directory/validate-release-lock.sh" \
  --lock "$lock_file" --source-sha "$source_sha" --sha256 "$lock_sha256" >/dev/null

temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
rendered="$temporary_directory/web-only.yaml"
kubectl kustomize "$repository_root/deploy/k8s/profiles/web-only" >"$rendered"

while IFS=$'\t' read -r component pull_ref; do
  COMPONENT="$component" PULL_REF="$pull_ref" yq -i '
    (.. | select(tag == "!!str")) |= sub(
      "[A-Za-z0-9._:/-]+/" + strenv(COMPONENT) + "@sha256:[a-f0-9]{64}";
      strenv(PULL_REF)
    )
  ' "$rendered"
done < <(jq -r '.images[] | [.component,.pull_ref] | @tsv' "$lock_file")

registry_push=$(jq -er '.registry.push' "$lock_file")
node_pull=$(jq -er '.registry.node_pull' "$lock_file")
repository_prefix=$(jq -er '.registry.repository_prefix' "$lock_file")
pull_registry_host=${node_pull%%/*}
agent_runner_ref=$(jq -er '.images[] | select(.component == "agent-runner") | .pull_ref' "$lock_file")
agent_runner_digest=$(jq -er '.images[] | select(.component == "agent-runner") | .digest' "$lock_file")
authority_ref=$(jq -er '.images[] | select(.component == "internal-rpc-authority") | .pull_ref' "$lock_file")
admission_ref=$(jq -er '.images[] | select(.component == "image-admission") | .pull_ref' "$lock_file")
admission_tools_ref=$(jq -er '.external_images[] | select(.component == "admission-tools") | .pull_ref' "$lock_file")
admission_tools_digest=$(jq -er '.external_images[] | select(.component == "admission-tools") | .digest' "$lock_file")
frontend_sha256=b6afd42430b15f2d2a4c5a02b919e98a525b785b1aaff16747d2f623364e39b6
oidc_origin=$(printf '%s\n' "$oidc_issuer" | sed -E 's#^(https://[^/]+).*$#\1#')

PUBLIC_HOST="$public_host" \
PUBLIC_ORIGIN="$public_origin" \
OIDC_ISSUER="$oidc_issuer" \
OIDC_JWKS_URL="$oidc_jwks_url" \
OIDC_CONNECT_ADDRESS="$oidc_connect_address" \
OIDC_TLS_SERVER_NAME="$oidc_tls_server_name" \
OIDC_ORIGIN="$oidc_origin" \
PULL_REGISTRY_HOST="$pull_registry_host" yq -i '
  (.. | select(tag == "!!str")) |= (
    sub("__MATTERCODEX_PUBLIC_HOST__"; strenv(PUBLIC_HOST)) |
    sub("__MATTERCODEX_PUBLIC_ORIGIN__"; strenv(PUBLIC_ORIGIN)) |
    sub("__MATTERCODEX_OIDC_ISSUER__"; strenv(OIDC_ISSUER)) |
    sub("__MATTERCODEX_OIDC_JWKS_URL__"; strenv(OIDC_JWKS_URL)) |
    sub("__MATTERCODEX_OIDC_CONNECT_ADDRESS__"; strenv(OIDC_CONNECT_ADDRESS)) |
    sub("__MATTERCODEX_OIDC_TLS_SERVER_NAME__"; strenv(OIDC_TLS_SERVER_NAME)) |
    sub("__MATTERCODEX_OIDC_ORIGIN__"; strenv(OIDC_ORIGIN)) |
    sub("registry-pull\\.invalid"; strenv(PULL_REGISTRY_HOST))
  )
' "$rendered"

LOCK_DIGEST="$lock_sha256" \
REGISTRY_PUSH="$registry_push" \
NODE_PULL="$node_pull" \
REPOSITORY_PREFIX="$repository_prefix" \
PULL_REGISTRY_HOST="$pull_registry_host" \
AGENT_RUNNER_REF="$agent_runner_ref" \
AGENT_RUNNER_DIGEST="$agent_runner_digest" \
AUTHORITY_REF="$authority_ref" \
ADMISSION_REF="$admission_ref" \
ADMISSION_TOOLS_REF="$admission_tools_ref" \
ADMISSION_TOOLS_DIGEST="$admission_tools_digest" \
FRONTEND_SHA256="$frontend_sha256" yq -i '
  (.. | select(tag == "!!str")) |= sub(
    "[A-Za-z0-9._:/-]+/image-admission-tools@sha256:[a-f0-9]{64}";
    strenv(ADMISSION_TOOLS_REF)
  ) |
  with(select(.kind == "ConfigMap" and .metadata.name == "mattercodex-image-admission-policy");
    .metadata.annotations."mattercodex.dev/admission-tools-sha256" = strenv(ADMISSION_TOOLS_DIGEST) |
    .data.toolsImage = strenv(ADMISSION_TOOLS_REF) |
    .data.admissionImage = strenv(ADMISSION_REF) |
    .data.authorityImage = strenv(AUTHORITY_REF) |
    .data.promotedPullRepository = (strenv(NODE_PULL) + "/" + strenv(REPOSITORY_PREFIX) + "/roles") |
    .data.pullRegistryHost = strenv(PULL_REGISTRY_HOST) |
    .data.pullCredentialGeneration = "1" |
    .data.nodeReadbackImage = strenv(AGENT_RUNNER_REF) |
    .data.roleImageInputRepository = (strenv(REGISTRY_PUSH) + "/" + strenv(REPOSITORY_PREFIX) + "/role-image-inputs") |
    .data.policyRevision = "1" |
    .data.policySHA256 = strenv(LOCK_DIGEST) |
    .data.trustedRoleBaseRepository = (strenv(NODE_PULL) + "/" + strenv(REPOSITORY_PREFIX) + "/agent-runner") |
    .data.trustedRoleBaseDigest = strenv(AGENT_RUNNER_DIGEST) |
    .data.frontendSHA256 = strenv(FRONTEND_SHA256) |
    .data.roleRuntimeContractRevision = "1" |
    .data.roleRuntimeContractSHA256 = strenv(LOCK_DIGEST)
  ) |
  with(select(.kind == "ConfigMap" and .metadata.name == "role-image-builder-runtime");
    .data.ROLE_IMAGE_BUILDER_EXPECTED_TOOLCHAIN_SHA256 = strenv(LOCK_DIGEST)
  ) |
  with(select(.kind == "Deployment" and .metadata.name == "control-plane");
    .spec.template.metadata.annotations."mattercodex.dev/agent-runtime-image-digest" = strenv(AGENT_RUNNER_DIGEST)
  )
' "$rendered"

# Canonicalize duplicate resources before validation. The web-only aggregate may
# include a shared ConfigMap through more than one component base, but the bytes
# must be identical.
yq -o=json '.' "$rendered" | jq -sc '
  map(select(.kind != null)) as $all |
  ($all | group_by([.apiVersion,.kind,(.metadata.namespace // ""),.metadata.name]) |
    map(select((map(tojson) | unique | length) > 1))) as $conflicts |
  if ($conflicts | length) > 0 then error("conflicting resource identities")
  else $all | unique_by([.apiVersion,.kind,(.metadata.namespace // ""),.metadata.name])[] end
' | yq -p=json -P >"$output"

if rg -n 'sha256:0{64}' "$output" >/dev/null; then
  fail 'render contains a zero image digest'
fi
if rg -n '__MATTERCODEX_[A-Z0-9_]+__|admission-tools\.invalid|registry-pull\.invalid|https://control\.invalid' "$output" >/dev/null; then
  fail 'render contains an unresolved deployment placeholder'
fi
if rg -n '\$\{[A-Z][A-Z0-9_]*IMAGE[A-Z0-9_]*\}' "$output" >/dev/null; then
  fail 'render contains an unresolved image variable'
fi

allowed_images="$temporary_directory/allowed-images.txt"
jq -r '.images[].pull_ref,.external_images[].pull_ref' "$lock_file" >"$allowed_images"
yq -N -r '.. | select(has("image")) | .image' "$output" | while IFS= read -r image_ref; do
  case "$image_ref" in
    */"$repository_prefix"/*)
      grep -Fx -- "$image_ref" "$allowed_images" >/dev/null ||
        fail "internal image is outside the release lock: $image_ref"
      ;;
  esac
  [[ "$image_ref" == *@sha256:* ]] || fail "mutable image reference is forbidden: $image_ref"
done

printf 'Web-only render created: %s\n' "$output"
