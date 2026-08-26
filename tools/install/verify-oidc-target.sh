#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex OIDC target verification failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 --context <name> --namespace <name> --pod-name <label>" \
    '  --pod-component <label> --target-port <port>' >&2
}

context=""
namespace=""
pod_name=""
pod_component=""
target_port=""
while (($# > 0)); do
  case "$1" in
    --context) context="${2:-}"; shift 2 ;;
    --namespace) namespace="${2:-}"; shift 2 ;;
    --pod-name) pod_name="${2:-}"; shift 2 ;;
    --pod-component) pod_component="${2:-}"; shift 2 ;;
    --target-port) target_port="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

label_pattern='^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$'
[[ -n "$context" ]] || fail 'exact context is required'
[[ "$namespace" =~ $label_pattern && "$pod_name" =~ $label_pattern &&
  "$pod_component" =~ $label_pattern ]] || fail 'OIDC selector is invalid'
[[ "$target_port" =~ ^[1-9][0-9]{0,4}$ ]] && ((10#$target_port <= 65535)) ||
  fail 'OIDC target port is invalid'
for command_name in jq kubectl; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ "$(kubectl config current-context)" == "$context" ]] ||
  fail 'current Kubernetes context mismatch'

selector="app.kubernetes.io/name=$pod_name,app.kubernetes.io/component=$pod_component"
pods=$(kubectl --context "$context" -n "$namespace" get pods -l "$selector" -o json) ||
  fail 'read OIDC target pods'
jq -e --argjson target_port "$target_port" '
  any(.items[]?;
    .status.phase == "Running" and
    any(.status.conditions[]?; .type == "Ready" and .status == "True") and
    any(.spec.containers[]?.ports[]?;
      .protocol == "TCP" and .containerPort == $target_port))
' <<<"$pods" >/dev/null ||
  fail 'no ready OIDC pod matches the exact selector and target port'

printf 'Kodex OIDC target verification completed: namespace=%s selector=%s port=%s\n' \
  "$namespace" "$selector" "$target_port"
