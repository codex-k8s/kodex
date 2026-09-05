#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Local profile selection test failed: %s\n' "$*" >&2
  exit 1
}

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
resolver="$root/tools/dev/resolve-local-profile.sh"
temporary=$(mktemp -d)
trap 'rm -rf -- "$temporary"' EXIT
render="$temporary/render.yaml"

[[ "$("$resolver" '' "$render")" == web-only ]] || fail 'fresh default differs'
[[ "$("$resolver" web-with-mattermost "$render")" == web-with-mattermost ]] || fail 'fresh optional profile differs'
for profile in web-only web-with-mattermost; do
  PROFILE="$profile" yq -n '
    {"apiVersion":"v1","kind":"ConfigMap",
     "metadata":{"name":"kodex-dev-source-provenance","namespace":"kodex-system"},
     "data":{"deploymentProfile":strenv(PROFILE)}}
  ' >"$render"
  [[ "$("$resolver" '' "$render")" == "$profile" ]] || fail 'stored profile is not reused'
  [[ "$("$resolver" "$profile" "$render")" == "$profile" ]] || fail 'explicit stored profile differs'
  other=web-only
  [[ "$profile" != web-only ]] || other=web-with-mattermost
  if "$resolver" "$other" "$render" >/dev/null 2>&1; then
    fail 'profile changed without explicit reset'
  fi
done
for mutation in '.data.deploymentProfile = "unknown"' '.data.deploymentProfile = null' '.kind = "Secret"'; do
  yq "$mutation" "$render" >"$temporary/bad.yaml"
  if "$resolver" '' "$temporary/bad.yaml" >/dev/null 2>&1; then
    fail "malformed provenance was accepted: $mutation"
  fi
done
for invalid in unknown ../web-only WEB-ONLY; do
  if "$resolver" "$invalid" "$temporary/absent.yaml" >/dev/null 2>&1; then
    fail 'unknown requested profile was accepted'
  fi
done
yq 'del(.data.deploymentProfile)' "$render" >"$temporary/legacy.yaml"
[[ "$("$resolver" '' "$temporary/legacy.yaml")" == web-only ]] || fail 'legacy render compatibility differs'
ln -s "$render" "$temporary/link.yaml"
if "$resolver" '' "$temporary/link.yaml" >/dev/null 2>&1; then
  fail 'render symlink was accepted'
fi
yq -n 'null' >"$temporary/invalid.yaml"
if "$resolver" '' "$temporary/invalid.yaml" >/dev/null 2>&1; then
  fail 'missing provenance was accepted'
fi

install -d -m 0700 "$temporary/state"
cp "$render" "$temporary/state/render.yaml"
printf '{"version":1}\n' >"$temporary/state/authority-source-state.json"
printf 'fixture\n' >"$temporary/kubeconfig"
down_arguments=(down --kubeconfig "$temporary/kubeconfig" --context fixture-local --state-directory "$temporary/state")
fixture_environment=(
  "PATH=$root/scripts/tests/fixtures/local-profile:$PATH"
  KODEX_DEV_TLS_MODE=local-ca
  KODEX_DEV_CONFIRM_DOWN=I_UNDERSTAND_THIS_REMOVES_KODEX_FROM_THE_BOUND_DISPOSABLE_CLUSTER
)
if env "${fixture_environment[@]}" KODEX_TEST_LOCAL_DOWN_FAIL=true \
  "$root/dev.sh" "${down_arguments[@]}" >/dev/null 2>&1; then
  fail 'failed reset was accepted'
fi
[[ -f "$temporary/state/render.yaml" && -f "$temporary/state/authority-source-state.json" ]] ||
  fail 'failed reset removed the profile or authority marker'
env "${fixture_environment[@]}" "$root/dev.sh" "${down_arguments[@]}" >/dev/null
[[ ! -e "$temporary/state/render.yaml" && ! -e "$temporary/state/authority-source-state.json" ]] ||
  fail 'successful reset retained the old profile or authority marker'
[[ "$("$resolver" web-only "$temporary/state/render.yaml")" == web-only ]] ||
  fail 'explicit reset did not permit selecting a new profile'
printf 'Local profile selection test passed\n'
