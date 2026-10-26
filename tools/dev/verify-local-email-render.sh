#!/usr/bin/env bash
set -euo pipefail

render=${1:?render path is required}
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
[[ -f "$render" && -s "$render" && ! -L "$render" ]] || {
  printf 'Local EMAIL render path is invalid\n' >&2
  exit 1
}
targets=$(yq -r '
  select(.kind == "ConfigMap" and .metadata.name == "internal-rpc-authority-publisher-target-registry") |
  .data."key-delivery-targets.yaml"
' "$render" | yq -o=json -I=0 '.targets' | jq '[.[] | select(.workload_id == "email-bridge")]')
# Expected policies принадлежат полным repo-owned kustomizations, включая components.
# Ошибка любой базы прерывает subshell даже при отключённом errexit внутри $(...).
policies=$(
  for capability in email-bridge email-bridge-data email-bridge-migration; do
    kubectl kustomize "$root/deploy/k8s/base/$capability" |
      yq -o=json -I=0 'select(.kind == "NetworkPolicy")' |
      jq -sc 'if length > 0 then .[] else error("expected NetworkPolicy source is empty") end' || exit 1
  done | jq -s '.'
)
mail_digest=$(yq -o=json -I=0 'select(.kind == "ConfigMap" and .data."mail-policy.json" != null)' "$render" |
  jq -sj 'if length == 1 then .[0].data["mail-policy.json"] else error("mail policy count is invalid") end' |
  sha256sum | awk '{print $1}')
admission=$(jq '.items' "$root/deploy/k8s/base/egress-gateway/mail/publication-admission.json")
yq -o=json -I=0 '.' "$render" | jq -s -e \
  --arg mailDigest "$mail_digest" \
  --argjson admission "$admission" \
  --argjson targets "${targets:-[]}" --argjson policies "$policies" \
  -f "$root/tools/dev/verify-local-email-render.jq" >/dev/null || {
    printf 'Local EMAIL runtime, migration, TLS or authority render is incomplete\n' >&2
    exit 1
  }
