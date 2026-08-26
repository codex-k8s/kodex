#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'NATS runtime credential materialization failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf 'Usage: %s --context <context> --material-directory <path>\n' "$0" >&2
}

context=""
material_directory=""
while (($# > 0)); do
  case "$1" in
    --context) context="${2:-}"; shift 2 ;;
    --material-directory) material_directory="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$context" ]] || fail 'context is required'
[[ "$material_directory" == /* && -d "$material_directory" && ! -L "$material_directory" ]] ||
  fail 'material directory is invalid'
command -v kubectl >/dev/null 2>&1 || fail 'kubectl is required'
[[ "$(kubectl config current-context)" == "$context" ]] || fail 'Kubernetes context mismatch'

for required_file in \
  "$material_directory/nats/operator.jwt" \
  "$material_directory/nats/system-account.public" \
  "$material_directory/nats/system-account.jwt" \
  "$material_directory/nats/account.public" \
  "$material_directory/nats/account.jwt" \
  "$material_directory/projections/control-plane-nats/user.creds" \
  "$material_directory/projections/control-plane-nats-bootstrap/user.creds" \
  "$material_directory/projections/control-api-gateway-nats/user.creds"; do
  [[ -s "$required_file" && ! -L "$required_file" ]] || fail "required material is invalid: $required_file"
done

kubectl create namespace kodex-system --dry-run=client -o yaml |
  kubectl apply --server-side --field-manager=kodex-install -f - >/dev/null

apply_secret() {
  local name=$1
  shift
  kubectl -n kodex-system create secret generic "$name" "$@" --dry-run=client -o yaml |
    kubectl apply --server-side --force-conflicts --field-manager=kodex-install -f - >/dev/null
}

apply_secret kodex-nats-credentials \
  --from-file=operator.jwt="$material_directory/nats/operator.jwt" \
  --from-file=system-account.public="$material_directory/nats/system-account.public" \
  --from-file=system-account.jwt="$material_directory/nats/system-account.jwt" \
  --from-file=account.public="$material_directory/nats/account.public" \
  --from-file=account.jwt="$material_directory/nats/account.jwt"
apply_secret control-plane-nats \
  --from-file=user.creds="$material_directory/projections/control-plane-nats/user.creds"
apply_secret control-plane-nats-bootstrap \
  --from-file=user.creds="$material_directory/projections/control-plane-nats-bootstrap/user.creds"
apply_secret control-api-gateway-nats \
  --from-file=user.creds="$material_directory/projections/control-api-gateway-nats/user.creds"

for contract in \
  'kodex-nats-credentials:account.jwt,account.public,operator.jwt,system-account.jwt,system-account.public' \
  'control-plane-nats:user.creds' \
  'control-plane-nats-bootstrap:user.creds' \
  'control-api-gateway-nats:user.creds'; do
  name=${contract%%:*}
  expected=${contract#*:}
  actual=$(kubectl -n kodex-system get secret "$name" -o json |
    jq -er '.data | keys | sort | join(",")')
  [[ "$actual" == "$expected" ]] || fail "Kubernetes Secret key readback mismatch: $name"
done

if kubectl -n kodex-system get statefulset kodex-nats >/dev/null 2>&1; then
  kubectl -n kodex-system rollout restart statefulset/kodex-nats >/dev/null
  kubectl -n kodex-system rollout status statefulset/kodex-nats --timeout=5m >/dev/null ||
    fail 'NATS rollout after credential rotation failed'
fi
for deployment in control-plane control-api-gateway; do
  if kubectl -n kodex-system get deployment "$deployment" >/dev/null 2>&1; then
    kubectl -n kodex-system rollout restart "deployment/$deployment" >/dev/null
    kubectl -n kodex-system rollout status "deployment/$deployment" --timeout=5m >/dev/null ||
      fail "workload rollout after NATS credential rotation failed: $deployment"
  fi
done

printf 'NATS runtime credentials materialized without secret output\n'
