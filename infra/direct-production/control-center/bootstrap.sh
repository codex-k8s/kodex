#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'Direct production Control Center bootstrap failed: %s\n' "$*" >&2; exit 1; }
usage() { printf 'Usage: %s --context <exact-context> --mode apply|readback\n' "$0" >&2; }

context=""
mode=""
while (($# > 0)); do
  case "$1" in
    --context) context="${2:-}"; shift 2 ;;
    --mode) mode="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$context" ]] || fail "exact Kubernetes context is required"
case "$mode" in apply|readback) ;; *) fail "mode must be apply or readback" ;; esac
for command_name in curl jq kubectl sha256sum yq; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ "$(kubectl config current-context)" == "$context" ]] || fail "Kubernetes context mismatch"

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT HUP INT TERM

render="$temporary_directory/control-center.yaml"
kubectl kustomize "$script_directory" >"$render"
config_sha256=$(yq -r 'select(.kind == "ConfigMap" and .metadata.name == "control-center-public-bridge") | .data."envoy.yaml"' "$render" | sha256sum | awk '{print $1}')
[[ "$config_sha256" =~ ^[a-f0-9]{64}$ ]] || fail "bridge configuration digest is invalid"
CONFIG_SHA256="$config_sha256" yq -i '
  with(select(.kind == "Deployment" and .metadata.name == "control-center-public-bridge");
    .spec.template.metadata.annotations."mattercodex.dev/config-sha256" = strenv(CONFIG_SHA256)
  )
' "$render"
kubectl apply --dry-run=client --validate=false -f "$render" >/dev/null

if [[ "$mode" == apply ]]; then
  kubectl --context "$context" apply --server-side --force-conflicts \
    --field-manager=mattercodex-control-center-bootstrap -f "$render" >/dev/null
  kubectl --context "$context" -n mattercodex-system wait \
    --for=condition=Ready certificate/control-center-public-tls --timeout=5m >/dev/null
  kubectl --context "$context" -n mattercodex-system rollout status \
    deployment/control-center-public-bridge --timeout=5m >/dev/null
fi

kubectl --context "$context" -n mattercodex-system get deployment control-center-public-bridge -o json |
  jq -e --arg config_sha256 "$config_sha256" '
    .spec.replicas == 2 and (.status.readyReplicas // 0) == 2 and
    (.status.availableReplicas // 0) == 2 and
    .spec.template.metadata.annotations."mattercodex.dev/config-sha256" == $config_sha256 and
    all(.spec.template.spec.containers[]; .image | test("@sha256:[a-f0-9]{64}$"))
  ' >/dev/null || fail "public bridge deployment readback failed"
kubectl --context "$context" -n mattercodex-system get certificate control-center-public-tls -o json |
  jq -e 'any(.status.conditions[]?; .type == "Ready" and .status == "True")' >/dev/null ||
  fail "public certificate is not Ready"
kubectl --context "$context" -n mattercodex-system get ingress control-center-public -o json |
  jq -e '
    .spec.ingressClassName == "kodex-public" and
    .spec.tls == [{"hosts":["control.kodex.works"],"secretName":"control-center-public-tls"}] and
    .spec.rules[0].host == "control.kodex.works" and
    .spec.rules[0].http.paths[0].backend.service.name == "control-center-public-bridge"
  ' >/dev/null || fail "public ingress readback failed"

runtime_config=$(curl --fail --silent --show-error --max-time 10 \
  https://control.kodex.works/config/runtime-config.json)
jq -e '
  .apiBaseUrl == "https://control.kodex.works/api/v1" and
  .realtimeUrl == "wss://control.kodex.works/api/v1/realtime" and
  .oidc.authority == "https://sso.kodex.works/realms/mattercodex" and
  .oidc.clientId == "mattercodex-control-center"
' <<<"$runtime_config" >/dev/null || fail "public runtime configuration readback failed"
curl --fail --silent --show-error --max-time 10 https://control.kodex.works/readyz |
  jq -e '.status == "ready"' >/dev/null || fail "public readiness readback failed"

printf 'Direct production Control Center %s completed\n' "$mode"

