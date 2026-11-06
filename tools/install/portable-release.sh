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
provider_profile_arguments=()
previous_arguments=()
arguments=()
repair_egress_secret_access=false
component_image_profile=""
component_image_sha=""
base_release_lock=""
previous_component_image_profile=""
previous_component_image_sha=""
completion_receipt=""
completion_sha=""
allow_reconciled_completion=false
completion_arguments=()

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
    --repair-egress-secret-access) repair_egress_secret_access=true; shift ;;
    --bootstrap-completion-receipt) completion_receipt="${2:-}"; shift 2 ;;
    --bootstrap-completion-sha256) completion_sha="${2:-}"; shift 2 ;;
    --allow-reconciled-bootstrap-completion) allow_reconciled_completion=true; shift ;;
    --component-image-profile) component_image_profile="${2:-}"; shift 2 ;;
    --component-image-profile-sha256) component_image_sha="${2:-}"; shift 2 ;;
    --base-release-lock) base_release_lock="${2:-}"; shift 2 ;;
    --previous-component-image-profile) previous_component_image_profile="${2:-}"; shift 2 ;;
    --previous-component-image-profile-sha256) previous_component_image_sha="${2:-}"; shift 2 ;;
    --provider-apparmor-profile) provider_profile_arguments=(--provider-apparmor-profile "${2-}"); shift 2 ;;
    --buildkit-runtime-profile) buildkit_profile="${2:-}"; shift 2 ;;
    --oidc-ca-file|--ingress-deployment|--ingress-controller|--ingress-namespace|--ingress-service) arguments+=("$1" "${2:-}"); shift 2 ;;
    --help)
      printf '%s\n' 'Usage: portable-release.sh --mode plan|preflight|apply|upgrade|resume-authority-bootstrap|resume-authority-published|resume-workloads|readback --render <yaml> --render-sha256 <64-hex> [--context <exact-context>]' \
        '  [--oidc-ca-file <pem>] [--ingress-controller <name> --ingress-namespace <name>]' \
        '  [--preimage-directory <new-private-directory>; required for apply/upgrade]'
      printf '%s\n' '  [--previous-render <yaml> --previous-render-sha256 <64-hex> --buildkit-runtime-profile standard|nested-userns; resume-workloads only]'
      printf '%s\n' '  [--clamav-mode enabled|disabled; default enabled]'
      printf '%s\n' '  [--repair-kubernetes-api-endpoints; explicit workload resume repair from pinned endpoint anchors]'
      printf '%s\n' '  [--bootstrap-completion-receipt <json> --bootstrap-completion-sha256 <64-hex> [--allow-reconciled-bootstrap-completion]; continuation/readback]'
      printf '%s\n' '  [--repair-egress-secret-access; explicit workload resume repair]'
      printf '%s\n' '  [--component-image-profile <json> --component-image-profile-sha256 <64-hex> --base-release-lock <json>; workload resume only]'
      printf '%s\n' '  [--previous-component-image-profile <json> --previous-component-image-profile-sha256 <64-hex>; exact component predecessor]'
      printf '%s\n' '  [--provider-apparmor-profile <empty|kodex-provider-runtime>; explicit runtime-only projection]'
      exit 0 ;;
    *) fail 'unsupported argument' ;;
  esac
done
case "$mode" in plan|preflight|apply|upgrade|resume-authority-bootstrap|resume-authority-published|resume-workloads|readback) ;; *) fail 'mode is invalid' ;; esac
if [[ "$mode" == upgrade ]]; then
  [[ -n "$completion_receipt" && "$completion_sha" =~ ^[a-f0-9]{64}$ && "$allow_reconciled_completion" == true ]] || fail 'upgrade requires an explicit forward migration completion receipt'
fi
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
  previous_arguments=("${provider_profile_arguments[@]}" --previous-render "$temporary/previous.yaml" --previous-render-sha256 "$previous_sha" --buildkit-runtime-profile "$buildkit_profile")
  [[ "$repair_api_endpoints" != true ]] || previous_arguments+=(--repair-kubernetes-api-endpoints)
  [[ "$repair_egress_secret_access" != true ]] || previous_arguments+=(--repair-egress-secret-access)
else
  [[ "$repair_egress_secret_access" == false ]] || fail 'egress secret repair only applies to workload resume'
  [[ "$repair_api_endpoints" == false ]] || fail 'API endpoint repair only applies to workload resume'
  [[ -z "$previous_render" && -z "$previous_sha" ]] || fail 'previous render only applies to workload resume'
  [[ "$buildkit_profile" == standard ]] || fail 'BuildKit profile must be materialized in the pinned render before fresh apply'
fi
if [[ -n "$previous_component_image_profile$previous_component_image_sha" ]]; then
  [[ "$mode" == resume-workloads && -n "$component_image_profile" && -f "$previous_component_image_profile" && ! -L "$previous_component_image_profile" && "$previous_component_image_sha" =~ ^[a-f0-9]{64}$ ]] || fail 'exact component predecessor requires workload target profile'
  cp -- "$previous_component_image_profile" "$temporary/previous-component-image.json"
  [[ "$(sha256sum "$temporary/previous-component-image.json" | awk '{print $1}')" == "$previous_component_image_sha" ]] || fail 'component predecessor digest mismatch'
  previous_arguments+=(--previous-component-image-profile "$temporary/previous-component-image.json" --previous-component-image-profile-sha256 "$previous_component_image_sha")
fi
if [[ -n "$component_image_profile$component_image_sha$base_release_lock" ]]; then
  [[ "$mode" == resume-workloads ]] || fail 'component image profile requires workload resume'
  [[ -f "$component_image_profile" && ! -L "$component_image_profile" && -f "$base_release_lock" && ! -L "$base_release_lock" && "$component_image_sha" =~ ^[a-f0-9]{64}$ ]] || fail 'exact component image profile and base lock are required'
  cp -- "$component_image_profile" "$temporary/component-image.json"
  cp -- "$base_release_lock" "$temporary/base-release-lock.json"
  [[ "$(sha256sum "$temporary/component-image.json" | awk '{print $1}')" == "$component_image_sha" ]] || fail 'component image profile digest mismatch'
  predecessor_arguments=()
  [[ -z "$previous_component_image_profile$previous_component_image_sha" ]] || predecessor_arguments=(--previous-component-image-profile "$temporary/previous-component-image.json" --previous-component-image-profile-sha256 "$previous_component_image_sha")
  python3 "$root/tools/install/component_image_profile.py" --check --render "$temporary/render.yaml" --configuration "$temporary/component-image.json" --configuration-sha256 "$component_image_sha" --base-release-lock "$temporary/base-release-lock.json" "${predecessor_arguments[@]}"
  previous_arguments+=(--component-image-profile "$temporary/component-image.json" --component-image-profile-sha256 "$component_image_sha" --base-release-lock "$temporary/base-release-lock.json")
fi
if [[ -n "$completion_receipt$completion_sha" || "$allow_reconciled_completion" == true ]]; then
  case "$mode" in upgrade|preflight|resume-authority-bootstrap|resume-authority-published|resume-workloads|readback) ;; *) fail 'completion receipt requires continuation or readback' ;; esac
  [[ -n "$completion_receipt" && "$completion_sha" =~ ^[a-f0-9]{64}$ ]] || fail 'exact completion receipt is required'
  # Проверка private mode/owner/hash до snapshot: cp не должен легализовать открытый input.
  python3 - "$root/tools/install" "$completion_receipt" "$completion_sha" "$allow_reconciled_completion" <<'PYLOAD'
import sys
sys.path.insert(0,sys.argv[1])
import bootstrap_completion
bootstrap_completion.load(sys.argv[2],sys.argv[3],sys.argv[4]=='true')
PYLOAD
  cp -- "$completion_receipt" "$temporary/bootstrap-completion.json"
  completion_arguments=(--bootstrap-completion-receipt "$temporary/bootstrap-completion.json" --bootstrap-completion-sha256 "$completion_sha")
  [[ "$allow_reconciled_completion" != true ]] || completion_arguments+=(--allow-reconciled-bootstrap-completion)
fi
if [[ "$mode" == upgrade ]]; then
  python3 "$root/tools/install/bootstrap_upgrade.py" verify-apply --render "$temporary/render.yaml" --context "$context" "${completion_arguments[@]}"
fi
if ((${#provider_profile_arguments[@]})); then
  python3 "$root/tools/install/provider_sandbox_profile.py" --check --render "$temporary/render.yaml" --profile "${provider_profile_arguments[1]}"
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
  timeout 180s python3 "$root/tools/install/authority_registry.py" preflight-resume --render "$temporary/render.yaml" --context "$context" "${completion_arguments[@]}"
fi
if [[ "$mode" == resume-authority-published ]]; then
  timeout 180s python3 "$root/tools/install/authority_registry.py" preflight-published --render "$temporary/render.yaml" --context "$context" "${completion_arguments[@]}"
fi
if [[ "$mode" == resume-workloads ]]; then
  timeout 180s python3 "$root/tools/install/authority_registry.py" preflight-workloads \
    --render "$temporary/render.yaml" --context "$context" "${previous_arguments[@]}" --clamav-mode "$clamav_mode" "${completion_arguments[@]}"
fi
if [[ "$mode" == apply || "$mode" == upgrade || "$mode" == resume-authority-bootstrap || "$mode" == resume-authority-published || "$mode" == resume-workloads ]]; then
  [[ -n "$preimage" && "$preimage" == /* && ! -e "$preimage" ]] || fail 'new absolute preimage directory is required'
  mkdir -m 0700 -- "$preimage"
  cp -- "$temporary/render.yaml" "$preimage/candidate-render.yaml"
  if [[ -n "$component_image_profile" ]]; then
    cp -- "$temporary/component-image.json" "$preimage/component-image.json"
    cp -- "$temporary/base-release-lock.json" "$preimage/base-release-lock.json"
    [[ -z "$previous_component_image_profile" ]] || cp -- "$temporary/previous-component-image.json" "$preimage/previous-component-image.json"
  fi
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
  if [[ "$mode" != apply ]] && ((${#completion_arguments[@]} == 0)); then
    python3 "$root/tools/install/bootstrap_completion.py" capture --render "$temporary/render.yaml" --context "$context" --output "$preimage/bootstrap-completion.json"
    completion_sha=$(sha256sum "$preimage/bootstrap-completion.json" | awk '{print $1}')
    completion_arguments=(--bootstrap-completion-receipt "$preimage/bootstrap-completion.json" --bootstrap-completion-sha256 "$completion_sha")
  elif ((${#completion_arguments[@]})); then
    cp -- "$temporary/bootstrap-completion.json" "$preimage/bootstrap-completion-input.json"
  fi
  # Preimage metadata не заменяет backup данных или разрешение на schema downgrade.
  phases=(prepare-preflight preflight apply)
  [[ "$mode" != upgrade ]] || phases=(prepare-preflight preflight upgrade)
  [[ "$mode" != resume-authority-bootstrap ]] || phases=(prepare-preflight preflight resume-authority-bootstrap)
  [[ "$mode" != resume-authority-published ]] || phases=(prepare-preflight preflight resume-authority-published)
  [[ "$mode" != resume-workloads ]] || phases=(prepare-preflight preflight resume-workloads)
  for phase in "${phases[@]}"; do
    phase_arguments=()
    [[ "$mode" != upgrade ]] || phase_arguments+=(--existing-email-projection-only)
    if [[ "$phase" == apply ]]; then phase_arguments=(--bootstrap-completion-output "$preimage/bootstrap-completion.json"); fi
    "$root/tools/install/deploy-platform.sh" --context "$context" --mode "$phase" \
      --render "$temporary/render.yaml" --public-tls-mode "$tls_mode" \
      --portable-inventory "$preimage/resource-inventory.json" --clamav-mode "$clamav_mode" "${arguments[@]}" "${previous_arguments[@]}" "${completion_arguments[@]}" "${phase_arguments[@]}"
  done
fi
if [[ "$mode" == apply && -f "$preimage/bootstrap-completion.json" ]]; then
  completion_sha=$(sha256sum "$preimage/bootstrap-completion.json" | awk '{print $1}')
  completion_arguments=(--bootstrap-completion-receipt "$preimage/bootstrap-completion.json" --bootstrap-completion-sha256 "$completion_sha")
fi
timeout 180s python3 "$root/tools/install/portable_state.py" readback \
  --context "$context" --render "$temporary/render.yaml" "${completion_arguments[@]}"
"$root/tools/install/deploy-platform.sh" --context "$context" --mode readback \
  --render "$temporary/render.yaml" --public-tls-mode "$tls_mode" --clamav-mode "$clamav_mode" "${arguments[@]}" "${completion_arguments[@]}"
