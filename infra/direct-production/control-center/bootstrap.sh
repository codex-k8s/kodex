#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'Direct production Control Center bootstrap failed: %s\n' "$*" >&2; exit 1; }
usage() { printf 'Usage: %s --context <exact-context> --mode apply|readback --public-host <dns-name> --oidc-host <dns-name>\n' "$0" >&2; }

context=""
mode=""
public_host=""
oidc_host=""
while (($# > 0)); do
  case "$1" in
    --context) context="${2:-}"; shift 2 ;;
    --mode) mode="${2:-}"; shift 2 ;;
    --public-host) public_host="${2:-}"; shift 2 ;;
    --oidc-host) oidc_host="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$context" ]] || fail "exact Kubernetes context is required"
case "$mode" in apply|readback) ;; *) fail "mode must be apply or readback" ;; esac
[[ "$public_host" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] || fail "public host is invalid"
[[ "$oidc_host" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] || fail "OIDC host is invalid"
public_origin="https://$public_host"
oidc_issuer="https://$oidc_host/realms/mattercodex"
for command_name in curl jq kubectl sha256sum yq; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ "$(kubectl config current-context)" == "$context" ]] || fail "Kubernetes context mismatch"

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
temporary_directory=$(mktemp -d)
validation_name=""
cleanup() {
  if [[ -n "$validation_name" ]]; then
    kubectl --context "$context" -n mattercodex-system delete \
      "pod/$validation_name" "configmap/$validation_name" \
      --ignore-not-found --wait=false >/dev/null 2>&1 || true
  fi
  rm -rf -- "$temporary_directory"
}
trap cleanup EXIT HUP INT TERM

render="$temporary_directory/control-center.yaml"
kubectl kustomize "$script_directory" >"$render"
PUBLIC_HOST="$public_host" yq -i '
  (.. | select(tag == "!!str")) |= sub("__MATTERCODEX_PUBLIC_HOST__"; strenv(PUBLIC_HOST))
' "$render"
envoy_config="$temporary_directory/envoy.yaml"
yq -r 'select(.kind == "ConfigMap" and .metadata.name == "control-center-public-bridge") | .data."envoy.yaml"' \
  "$render" >"$envoy_config"
config_sha256=$(sha256sum "$envoy_config" | awk '{print $1}')
[[ "$config_sha256" =~ ^[a-f0-9]{64}$ ]] || fail "bridge configuration digest is invalid"
bridge_image=$(yq -r '
  select(.kind == "Deployment" and .metadata.name == "control-center-public-bridge") |
  .spec.template.spec.containers[] | select(.name == "envoy") | .image
' "$render")
[[ "$bridge_image" =~ ^[^[:space:]]+@sha256:[a-f0-9]{64}$ ]] ||
  fail "bridge image must be pinned by digest"
CONFIG_SHA256="$config_sha256" yq -i '
  with(select(.kind == "Deployment" and .metadata.name == "control-center-public-bridge");
    .spec.template.metadata.annotations."mattercodex.dev/config-sha256" = strenv(CONFIG_SHA256)
  )
' "$render"
kubectl apply --dry-run=client --validate=false -f "$render" >/dev/null

if [[ "$mode" == apply ]]; then
  validation_name="control-center-envoy-validation-${config_sha256:0:8}-$$"
  kubectl --context "$context" -n mattercodex-system create configmap "$validation_name" \
    --from-file=envoy.yaml="$envoy_config" --dry-run=client -o yaml |
    kubectl --context "$context" apply --server-side \
      --field-manager=mattercodex-control-center-bootstrap -f - >/dev/null
  VALIDATION_NAME="$validation_name" BRIDGE_IMAGE="$bridge_image" yq '
    select(.kind == "Deployment" and .metadata.name == "control-center-public-bridge") |
    {
      "apiVersion": "v1",
      "kind": "Pod",
      "metadata": {
        "name": strenv(VALIDATION_NAME),
        "namespace": "mattercodex-system",
        "labels": {
          "app.kubernetes.io/name": "control-center-public-bridge",
          "app.kubernetes.io/component": "config-validation"
        }
      },
      "spec": .spec.template.spec
    } |
    .spec.restartPolicy = "Never" |
    .spec.containers[0].image = strenv(BRIDGE_IMAGE) |
    .spec.containers[0].args = ["--mode", "validate", "-c", "/etc/envoy/envoy.yaml"] |
    del(
      .spec.containers[0].ports,
      .spec.containers[0].startupProbe,
      .spec.containers[0].readinessProbe,
      .spec.containers[0].livenessProbe
    ) |
    .spec.volumes[] |=
      (select(.name == "config").configMap.name = strenv(VALIDATION_NAME))
  ' "$render" | kubectl --context "$context" apply -f - >/dev/null

  validation_deadline=$((SECONDS + 90))
  while true; do
    validation_phase=$(kubectl --context "$context" -n mattercodex-system get pod \
      "$validation_name" -o jsonpath='{.status.phase}')
    case "$validation_phase" in
      Succeeded) break ;;
      Failed)
        kubectl --context "$context" -n mattercodex-system logs "$validation_name" >&2 || true
        fail "pinned Envoy rejected bridge configuration"
        ;;
    esac
    if ((SECONDS >= validation_deadline)); then
      kubectl --context "$context" -n mattercodex-system logs "$validation_name" >&2 || true
      fail "pinned Envoy configuration validation timed out"
    fi
    sleep 2
  done
  kubectl --context "$context" -n mattercodex-system delete \
    "pod/$validation_name" "configmap/$validation_name" --wait=true >/dev/null
  validation_name=""

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
  jq -e --arg public_host "$public_host" '
    .spec.ingressClassName == "kodex-public" and
    .spec.tls == [{"hosts":[$public_host],"secretName":"control-center-public-tls"}] and
    .spec.rules[0].host == $public_host and
    .spec.rules[0].http.paths[0].backend.service.name == "control-center-public-bridge"
  ' >/dev/null || fail "public ingress readback failed"

runtime_config=$(curl --fail --silent --show-error --max-time 10 \
  "$public_origin/config/runtime-config.json")
jq -e --arg public_origin "$public_origin" --arg oidc_issuer "$oidc_issuer" '
  .apiBaseUrl == ($public_origin + "/api/v1") and
  .realtimeUrl == (($public_origin | sub("^https:"; "wss:")) + "/api/v1/realtime") and
  .oidc.authority == $oidc_issuer and
  .oidc.clientId == "mattercodex-control-center"
' <<<"$runtime_config" >/dev/null || fail "public runtime configuration readback failed"
curl --fail --silent --show-error --max-time 10 "$public_origin/readyz" |
  jq -e '.status == "ready"' >/dev/null || fail "public readiness readback failed"

printf 'Direct production Control Center %s completed\n' "$mode"
