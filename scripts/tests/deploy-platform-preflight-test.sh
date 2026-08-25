#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Deploy platform preflight test failed: %s\n' "$*" >&2
  exit 1
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
fake_bin="$temporary_directory/bin"
kubectl_log="$temporary_directory/kubectl.log"
render_file="$temporary_directory/release.yaml"
mkdir -p "$fake_bin"

cat >"$render_file" <<'YAML'
apiVersion: v1
kind: Namespace
metadata:
  name: kodex-system
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: examples.policy.kodex.dev
spec:
  group: policy.kodex.dev
  names:
    kind: Example
    plural: examples
  scope: Namespaced
  versions:
    - name: v1alpha1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
---
apiVersion: policy.kodex.dev/v1alpha1
kind: Example
metadata:
  name: excluded-custom-resource
  namespace: kodex-system
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: known-resource
  namespace: kodex-system
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: immutable-resource
  namespace: kodex-system
immutable: true
data:
  marker: exact
---
apiVersion: batch/v1
kind: Job
metadata:
  name: recreated-job
  namespace: kodex-system
spec:
  template:
    spec:
      restartPolicy: Never
      containers:
        - name: task
          image: registry.example.com/task@sha256:1111111111111111111111111111111111111111111111111111111111111111
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: staff-control-center-public
  namespace: kodex-system
spec:
  secretName: staff-control-center-public-tls
YAML

cat >"$fake_bin/kubectl" <<'BASH'
#!/usr/bin/env bash
set -euo pipefail

printf 'mode=%s args=' "${KODEX_DEPLOY_PUBLIC_TLS_MODE:-unset}" >>"$KUBECTL_LOG"
printf '%q ' "$@" >>"$KUBECTL_LOG"
printf '\n' >>"$KUBECTL_LOG"

if [[ "$*" == "config current-context" ]]; then
  printf 'kodex-test\n'
  exit 0
fi
if [[ " $* " == *' get certificate/staff-control-center-public '* ]]; then
  exit 1
fi
if [[ " $* " == *' get certificaterequests.cert-manager.io,orders.acme.cert-manager.io,challenges.acme.cert-manager.io '* ]]; then
  printf '{"items":[]}\n'
  exit 0
fi
if [[ " $* " == *' get secret '* ]]; then
  exit 0
fi
if [[ " $* " == *' apply '* ]]; then
  manifest=""
  while (($# > 0)); do
    if [[ "$1" == -f ]]; then
      manifest=${2:-}
      break
    fi
    shift
  done
  [[ -n "$manifest" && -s "$manifest" ]] || exit 11
  public=no
  custom=no
  rg -q 'name: staff-control-center-public' "$manifest" && public=yes
  rg -q 'name: excluded-custom-resource' "$manifest" && custom=yes
  printf 'manifest mode=%s public=%s custom=%s\n' \
    "${KODEX_DEPLOY_PUBLIC_TLS_MODE:-unset}" "$public" "$custom" >>"$KUBECTL_LOG"
  exit 0
fi
if [[ " $* " == *' create '* ]]; then
  manifest=""
  while (($# > 0)); do
    if [[ "$1" == -f ]]; then
      manifest=${2:-}
      break
    fi
    shift
  done
  [[ -n "$manifest" && -s "$manifest" ]] || exit 12
  immutable=no
  job=no
  rg -q 'immutable: true' "$manifest" && immutable=yes
  rg -q 'kind: Job' "$manifest" && job=yes
  printf 'create mode=%s immutable=%s job=%s\n' \
    "${KODEX_DEPLOY_PUBLIC_TLS_MODE:-unset}" "$immutable" "$job" >>"$KUBECTL_LOG"
  exit 0
fi
exit 0
BASH
chmod +x "$fake_bin/kubectl"

export PATH="$fake_bin:$PATH"
export KUBECTL_LOG=$kubectl_log

for tls_mode in enabled deferred; do
  for _ in 1 2; do
    "$repository_root/tools/install/deploy-platform.sh" \
      --context kodex-test --mode preflight --render "$render_file" \
      --public-tls-mode "$tls_mode" >/dev/null
  done
done

[[ "$(grep -c ' args=.* apply ' "$kubectl_log")" == 8 ]] ||
  fail 'repeated preflight did not execute both server-side apply phases'
[[ "$(grep -c ' args=.* apply .*--server-side .*--dry-run=server .*--field-manager=kodex-install ' \
  "$kubectl_log")" == 8 ]] ||
  fail 'preflight did not use server-side dry-run with the release field manager'
! grep -Fq -- '--dry-run=client' "$kubectl_log" ||
  fail 'client-side dry-run remains in platform preflight'
[[ "$(grep -c 'manifest mode=enabled public=yes custom=no' "$kubectl_log")" == 2 ]] ||
  fail 'enabled preflight did not validate the public Certificate exactly once per run'
! grep -q 'manifest mode=deferred public=yes' "$kubectl_log" ||
  fail 'deferred preflight submitted the public Certificate'
! grep -q 'manifest .* custom=yes' "$kubectl_log" ||
  fail 'preflight submitted a custom resource introduced by the same render'
[[ "$(grep -c ' args=.* create .*--dry-run=server .*--field-manager=kodex-install ' \
  "$kubectl_log")" == 8 ]] ||
  fail 'recreated resources were not validated with server-side create dry-run'
[[ "$(grep -c 'create mode=.* immutable=yes job=no' "$kubectl_log")" == 4 ]] ||
  fail 'immutable ConfigMaps were not validated independently'
[[ "$(grep -c 'create mode=.* immutable=no job=yes' "$kubectl_log")" == 4 ]] ||
  fail 'Jobs were not validated independently'
! grep -q ' args=.* delete ' "$kubectl_log" ||
  fail 'preflight mutated the Kubernetes API'

: >"$kubectl_log"
"$repository_root/tools/install/deploy-platform.sh" \
  --context kodex-test --mode defer-public-tls --render "$render_file" \
  --public-tls-mode deferred >/dev/null
grep -q ' delete certificate/staff-control-center-public ' "$kubectl_log" ||
  fail 'deferred mode did not retire the public Certificate'
! grep -q ' args=.* apply ' "$kubectl_log" ||
  fail 'deferred mode applied release resources'

printf 'Deploy platform preflight test passed\n'
