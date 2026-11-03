#!/usr/bin/env bash
set -euo pipefail

# Локальный CI/CD потребляет уже проверенный render по точному digest.
# Ни токен GitHub, ни изменение Variables/Secrets/runner policy здесь не нужны.
fail() { printf 'Portable release failed: %s\n' "$*" >&2; exit 1; }
mode=""
context=""
render=""
expected_sha=""
preimage=""
previous_render=""
previous_sha=""
buildkit_profile=standard
repair_api_endpoints=false
clamav_mode=enabled
previous_arguments=()
arguments=()
while (($#)); do
  case "$1" in
    --mode) mode="${2:-}"; shift 2 ;;
    --context) context="${2:-}"; shift 2 ;;
    --render) render="${2:-}"; shift 2 ;;
    --render-sha256) expected_sha="${2:-}"; shift 2 ;;
    --preimage-directory) preimage="${2:-}"; shift 2 ;;
    --previous-render) previous_render="${2:-}"; shift 2 ;;
    --previous-render-sha256) previous_sha="${2:-}"; shift 2 ;;
    --clamav-mode) clamav_mode="${2:-}"; shift 2 ;;
    --repair-kubernetes-api-endpoints) repair_api_endpoints=true; shift ;;
    --buildkit-runtime-profile) buildkit_profile="${2:-}"; shift 2 ;;
    --oidc-ca-file|--ingress-deployment|--ingress-controller|--ingress-namespace|--ingress-service) arguments+=("$1" "${2:-}"); shift 2 ;;
    --help)
      printf '%s\n' 'Usage: portable-release.sh --mode plan|preflight|apply|resume-authority-bootstrap|resume-authority-published|resume-workloads|readback --render <yaml> --render-sha256 <64-hex> [--context <exact-context>]' \
        '  [--oidc-ca-file <pem>] [--ingress-controller <name> --ingress-namespace <name>]' \
        '  [--preimage-directory <new-private-directory>; required for apply]'
      printf '%s\n' '  [--previous-render <yaml> --previous-render-sha256 <64-hex> --buildkit-runtime-profile standard|nested-userns; resume-workloads only]'
      printf '%s\n' '  [--clamav-mode enabled|disabled; default enabled]'
      printf '%s\n' '  [--repair-kubernetes-api-endpoints; explicit workload resume repair from pinned endpoint anchors]'
      exit 0 ;;
    *) fail 'unsupported argument' ;;
  esac
done
case "$mode" in plan|preflight|apply|resume-authority-bootstrap|resume-authority-published|resume-workloads|readback) ;; *) fail 'mode is invalid' ;; esac
case "$clamav_mode" in enabled|disabled) ;; *) fail 'ClamAV mode is invalid' ;; esac
[[ "$expected_sha" =~ ^[a-f0-9]{64}$ ]] || fail 'exact render digest is required'
[[ -f "$render" && ! -L "$render" ]] || fail 'regular render file is required'
[[ "$mode" == plan || -n "$context" ]] || fail 'exact context is required'
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
umask 077
temporary=$(mktemp -d)
trap 'rm -rf -- "$temporary"' EXIT
cp -- "$render" "$temporary/render.yaml"
[[ "$(sha256sum "$temporary/render.yaml" | awk '{print $1}')" == "$expected_sha" ]] || fail 'render digest mismatch'
if [[ "$mode" == resume-workloads ]]; then
  [[ -f "$previous_render" && ! -L "$previous_render" && "$previous_sha" =~ ^[a-f0-9]{64}$ ]] || fail 'exact previous render is required'
  cp -- "$previous_render" "$temporary/previous.yaml"
  [[ "$(sha256sum "$temporary/previous.yaml" | awk '{print $1}')" == "$previous_sha" ]] || fail 'previous render digest mismatch'
  case "$buildkit_profile" in standard|nested-userns) ;; *) fail 'BuildKit runtime profile is invalid' ;; esac
  previous_arguments=(--previous-render "$temporary/previous.yaml" --previous-render-sha256 "$previous_sha" --buildkit-runtime-profile "$buildkit_profile")
  [[ "$repair_api_endpoints" != true ]] || previous_arguments+=(--repair-kubernetes-api-endpoints)
else
  [[ "$repair_api_endpoints" == false ]] || fail 'API endpoint repair only applies to workload resume'
  [[ -z "$previous_render" && -z "$previous_sha" ]] || fail 'previous render only applies to workload resume'
  [[ "$buildkit_profile" == standard ]] || fail 'BuildKit profile must be materialized in the pinned render before fresh apply'
fi
timeout 60s python3 "$root/tools/install/portable_preflight.py" --plan \
  --render "$temporary/render.yaml" --sha256 "$expected_sha"
python3 "$root/tools/install/clamav_profile.py" --check --mode "$clamav_mode" --render "$temporary/render.yaml"
[[ "$mode" != plan ]] || exit 0
[[ "$(kubectl config current-context)" == "$context" ]] || fail 'current Kubernetes context mismatch'
timeout 60s python3 "$root/tools/install/portable_preflight.py" \
  --render "$temporary/render.yaml" --sha256 "$expected_sha" --context "$context" "${arguments[@]}"
[[ "$mode" != preflight ]] || exit 0
tls_mode=$(yq -r 'select(.kind == "ConfigMap" and .metadata.name == "kodex-installation-profile") | .data."profile.json"' "$temporary/render.yaml" | jq -r '.public_tls.mode')
[[ "$tls_mode" != managed ]] || tls_mode=enabled
if [[ "$mode" == resume-authority-bootstrap ]]; then
  timeout 180s python3 "$root/tools/install/authority_registry.py" preflight-resume --render "$temporary/render.yaml" --context "$context"
fi
if [[ "$mode" == resume-authority-published ]]; then
  timeout 180s python3 "$root/tools/install/authority_registry.py" preflight-published --render "$temporary/render.yaml" --context "$context"
fi
if [[ "$mode" == resume-workloads ]]; then
  timeout 180s python3 "$root/tools/install/authority_registry.py" preflight-workloads \
    --render "$temporary/render.yaml" --context "$context" "${previous_arguments[@]}" --clamav-mode "$clamav_mode"
fi
if [[ "$mode" == apply || "$mode" == resume-authority-bootstrap || "$mode" == resume-authority-published || "$mode" == resume-workloads ]]; then
  [[ -n "$preimage" && "$preimage" == /* && ! -e "$preimage" ]] || fail 'new absolute preimage directory is required'
  mkdir -m 0700 -- "$preimage"
  cp -- "$temporary/render.yaml" "$preimage/candidate-render.yaml"
  # Только metadata и image pins: никаких Secret, env values или DB dumps.
  kubectl --context "$context" --request-timeout=15s get namespace kodex-system kodex-runtime --ignore-not-found -o json |
    jq '{items:[.items[] | select(.metadata.name == "kodex-system" or .metadata.name == "kodex-runtime") | {metadata:{name:.metadata.name,uid:.metadata.uid,resourceVersion:.metadata.resourceVersion}}]}' >"$preimage/namespaces.json"
  for namespace in kodex-system kodex-runtime; do
    if jq -e --arg namespace "$namespace" 'any(.items[]; .metadata.name == $namespace)' "$preimage/namespaces.json" >/dev/null; then
      kubectl --context "$context" -n "$namespace" --request-timeout=15s get deployments,statefulsets,pvc -o json |
        jq '{items:[.items[] | {kind,metadata:{name:.metadata.name,uid:.metadata.uid,resourceVersion:.metadata.resourceVersion},images:[.spec.template.spec.containers[]?.image]}]}' >"$preimage/$namespace-metadata.json"
    fi
  done
  timeout 180s python3 "$root/tools/install/portable_state.py" inventory \
    --context "$context" --render "$temporary/render.yaml" --inventory "$preimage/resource-inventory.json"
  # Preimage metadata не заменяет backup данных или разрешение на schema downgrade.
  phases=(prepare-preflight preflight apply)
  [[ "$mode" != resume-authority-bootstrap ]] || phases=(prepare-preflight preflight resume-authority-bootstrap)
  [[ "$mode" != resume-authority-published ]] || phases=(prepare-preflight preflight resume-authority-published)
  [[ "$mode" != resume-workloads ]] || phases=(prepare-preflight preflight resume-workloads)
  for phase in "${phases[@]}"; do
    "$root/tools/install/deploy-platform.sh" --context "$context" --mode "$phase" \
      --render "$temporary/render.yaml" --public-tls-mode "$tls_mode" \
      --portable-inventory "$preimage/resource-inventory.json" --clamav-mode "$clamav_mode" "${arguments[@]}" "${previous_arguments[@]}"
  done
fi
timeout 180s python3 "$root/tools/install/portable_state.py" readback \
  --context "$context" --render "$temporary/render.yaml"
"$root/tools/install/deploy-platform.sh" --context "$context" --mode readback \
  --render "$temporary/render.yaml" --public-tls-mode "$tls_mode" --clamav-mode "$clamav_mode" "${arguments[@]}"
