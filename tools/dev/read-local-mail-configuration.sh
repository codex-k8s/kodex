#!/usr/bin/env bash
set -euo pipefail
umask 077

fail() { printf 'Local mail source readback failed: %s\n' "$*" >&2; exit 1; }
[[ $# == 1 && "$1" == /* && ! -L "$1" ]] || fail 'exact output path is required'
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
temporary=$(mktemp -d)
trap 'rm -rf -- "$temporary"' EXIT
# Содержимое Secret не попадает в stdout, диагностику или render.
kubectl -n kodex-system get secret/email-bridge-mailbox-projection \
  --ignore-not-found --request-timeout=20s -o json >"$temporary/secret.json" || fail 'Secret discovery failed'
if [[ ! -s "$temporary/secret.json" ]]; then
  yq -r 'select(.kind == "Secret") | .stringData."mailboxes.json"' \
    "$root/deploy/k8s/base/control-plane/email-projection.yaml" >"$temporary/source.json"
else
  jq -er '
    if .kind == "Secret" and .metadata.name == "email-bridge-mailbox-projection" and
      .metadata.namespace == "kodex-system" and
      .metadata.labels["app.kubernetes.io/managed-by"] == "control-plane" and
      (.metadata.uid | type == "string" and length > 0) and .type == "Opaque" and .immutable != true
    then .data["mailboxes.json"] | @base64d else error("invalid owner") end
  ' "$temporary/secret.json" >"$temporary/source.json" 2>/dev/null || fail 'Secret ownership or document is invalid'
fi
jq -e '.version == "email-bridge/v1" and (.revision | type == "number") and .revision >= 1 and
  (.mailboxes | type == "array")' "$temporary/source.json" >/dev/null 2>&1 || fail 'mailbox document is invalid'
mv -- "$temporary/source.json" "$1"
