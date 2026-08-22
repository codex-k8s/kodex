#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Provider credential materialization failed: %s\n' "$*" >&2
  exit 1
}

namespace="${MATTERCODEX_NAMESPACE:-mattercodex-system}"
context="${MATTERCODEX_KUBE_CONTEXT:-}"
auth_file="${MATTERCODEX_PROVIDER_AUTH_FILE:-}"
revision="${MATTERCODEX_PROVIDER_CREDENTIAL_REVISION:-1}"

[[ -n "$context" ]] || fail 'MATTERCODEX_KUBE_CONTEXT is required'
[[ "$namespace" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]] || fail 'MATTERCODEX_NAMESPACE is invalid'
[[ "$revision" =~ ^[1-9][0-9]*$ ]] || fail 'MATTERCODEX_PROVIDER_CREDENTIAL_REVISION is invalid'
[[ -n "$auth_file" && "$auth_file" = /* && -f "$auth_file" && ! -L "$auth_file" ]] || fail 'MATTERCODEX_PROVIDER_AUTH_FILE must reference a regular absolute file'
[[ $(stat -c '%a' "$auth_file") =~ ^[46]00$ ]] || fail 'provider authentication file must not be group or world readable'

secret_name="runtime-provider-openai-default-r${revision}"
metadata_name="runtime-provider-openai-default-metadata"
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
digest=$(sha256sum -- "$auth_file" | awk '{print $1}')
[[ "$digest" =~ ^[a-f0-9]{64}$ ]] || fail 'provider authentication digest is invalid'
printf '%s\n' "$digest" >"$temporary_directory/auth.sha256"
chmod 0600 "$temporary_directory/auth.sha256"

kubectl --context "$context" --namespace "$namespace" create secret generic "$secret_name" \
  --from-file="auth.json=$auth_file" \
  --from-file="auth.sha256=$temporary_directory/auth.sha256" \
  --dry-run=client -o json |
  jq --arg revision "$revision" '.immutable = true |
      .metadata.labels = ((.metadata.labels // {}) + {
        "app.kubernetes.io/managed-by": "mattercodex-provider-materializer",
        "mattercodex.dev/provider": "openai-codex",
        "mattercodex.dev/credential-revision": $revision
      })' \
  | kubectl --context "$context" apply --server-side --field-manager=mattercodex-provider-materializer -f - >/dev/null

readback=$(kubectl --context "$context" --namespace "$namespace" get secret "$secret_name" -o json)
readback_digest=$(jq -er '.data["auth.sha256"] | @base64d' <<<"$readback" | tr -d '\r\n')
[[ $(jq -r '.immutable' <<<"$readback") == true && "$readback_digest" == "$digest" ]] || fail 'provider credential readback is invalid'
secret_uid=$(jq -er '.metadata.uid' <<<"$readback")
secret_resource_version=$(jq -er '.metadata.resourceVersion' <<<"$readback")
[[ "$secret_uid" =~ ^[a-f0-9-]{36}$ && -n "$secret_resource_version" ]] || fail 'provider credential identity readback is invalid'

kubectl --context "$context" --namespace "$namespace" create configmap "$metadata_name" \
  --from-literal="secretName=$secret_name" \
  --from-literal="secretUID=$secret_uid" \
  --from-literal="secretResourceVersion=$secret_resource_version" \
  --from-literal="contentSHA256=$digest" \
  --dry-run=client -o yaml |
  kubectl --context "$context" apply --server-side --field-manager=mattercodex-provider-materializer -f - >/dev/null

printf 'Provider credential revision materialized without exposing credential values\n'
