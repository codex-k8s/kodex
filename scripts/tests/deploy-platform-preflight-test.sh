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
  labels:
    app.kubernetes.io/part-of: kodex
    kodex.dev/owner-intent: "true"
spec:
  secretName: staff-control-center-public-tls
  dnsNames:
    - control.example.com
    - control-recovery.example.com
  issuerRef:
    name: letsencrypt-production
    kind: ClusterIssuer
---
apiVersion: supplychain.kodex.dev/v1alpha1
kind: ImageAdmissionPolicyParameters
metadata:
  name: kodex-image-admission-policy
  namespace: kodex-system
spec:
  revision: exact
---
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicyBinding
metadata:
  name: kodex-image-admission-controller-jobs
  labels:
    app.kubernetes.io/part-of: kodex
    kodex.dev/environment: production
    kodex.dev/profile: web-only
spec:
  policyName: kodex-image-admission-controller-jobs
  validationActions: [Deny]
  paramRef:
    name: kodex-image-admission-policy
    namespace: kodex-system
    parameterNotFoundAction: Deny
  matchResources:
    namespaceSelector:
      matchLabels:
        kubernetes.io/metadata.name: kodex-system
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
if [[ " $* " == *' get imageadmissionpolicyparameters.supplychain.kodex.dev/kodex-image-admission-policy '* ]]; then
  [[ "${KODEX_TEST_PARAMETER_PRESENT:-false}" == true ]]
  exit
fi
if [[ " $* " == *' get validatingadmissionpolicybinding.admissionregistration.k8s.io/kodex-image-admission-controller-jobs '* ]]; then
  [[ ! -e "$KODEX_TEST_BINDING_REMOVED" && "${KODEX_TEST_BINDING_PRESENT:-false}" == true ]] || exit 1
  policy_name=kodex-image-admission-controller-jobs
  [[ "${KODEX_TEST_BINDING_MISMATCH:-false}" != true ]] || policy_name=unexpected-policy
  jq -n --arg policy_name "$policy_name" '{
    metadata:{labels:{
      "app.kubernetes.io/part-of":"kodex",
      "kodex.dev/environment":"production",
      "kodex.dev/profile":"web-only"
    }},
    spec:{
      policyName:$policy_name,
      validationActions:["Deny"],
      paramRef:{name:"kodex-image-admission-policy",namespace:"kodex-system",
        parameterNotFoundAction:"Deny"},
      matchResources:{
        matchPolicy:"Equivalent",
        namespaceSelector:{matchLabels:{
          "kubernetes.io/metadata.name":"kodex-system"
        }},
        objectSelector:{}
      }
    }
  }'
  exit 0
fi
if [[ " $* " == *' get clusterissuer.cert-manager.io/letsencrypt-production '* ]]; then
  ready=${KODEX_TEST_CLUSTER_ISSUER_READY:-true}
  jq -n --arg ready "$ready" '{
    apiVersion:"cert-manager.io/v1",kind:"ClusterIssuer",
    metadata:{name:"letsencrypt-production"},
    status:{conditions:[{type:"Ready",status:(if $ready == "true" then "True" else "False" end)}]}
  }'
  exit 0
fi
if [[ " $* " == *' delete validatingadmissionpolicybinding.admissionregistration.k8s.io/kodex-image-admission-controller-jobs '* ]]; then
  : >"$KODEX_TEST_BINDING_REMOVED"
  exit 0
fi
if [[ " $* " == *' get certificate/staff-control-center-public '* ]]; then
  if [[ "${KODEX_TEST_PUBLIC_CERT_PRESENT:-false}" == true &&
    ! -e "$KODEX_TEST_PUBLIC_CERT_REMOVED" ]]; then
    owner=${KODEX_TEST_PUBLIC_CERT_OWNER:-true}
    jq -n --arg owner "$owner" '{
      apiVersion:"cert-manager.io/v1",kind:"Certificate",
      metadata:{name:"staff-control-center-public",namespace:"kodex-system",labels:{
        "app.kubernetes.io/part-of":(if $owner == "true" then "kodex" else "foreign" end),
        "kodex.dev/owner-intent":"true"
      }}
    }'
    exit 0
  fi
  exit 1
fi
if [[ " $* " == *' get certificaterequests.cert-manager.io,orders.acme.cert-manager.io,challenges.acme.cert-manager.io '* ]]; then
  if [[ "${KODEX_TEST_PUBLIC_DESCENDANT_ONCE:-false}" == true &&
    ! -e "$KODEX_TEST_PUBLIC_DESCENDANT_OBSERVED" ]]; then
    : >"$KODEX_TEST_PUBLIC_DESCENDANT_OBSERVED"
    printf '{"items":[{"metadata":{"name":"staff-control-center-public-1"}}]}\n'
    exit 0
  fi
  printf '{"items":[]}\n'
  exit 0
fi
if [[ " $* " == *' get secret '* ]]; then
  exit 0
fi
if [[ " $* " == *' get configmap/immutable-resource '* ]]; then
  exit 1
fi
if [[ " $* " == *' wait --for=condition=Ready certificate/staff-control-center-public '* &&
  "${KODEX_TEST_PUBLIC_CERT_READY:-true}" != true ]]; then
  exit 1
fi
if [[ " $* " == *' delete certificate/staff-control-center-public '* ]]; then
  : >"$KODEX_TEST_PUBLIC_CERT_REMOVED"
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

cat >"$fake_bin/dig" <<'BASH'
#!/usr/bin/env bash
set -euo pipefail
printf '%q ' "$@" >>"$KODEX_TEST_DNS_LOG"
printf '\n' >>"$KODEX_TEST_DNS_LOG"
case " $* " in
  *' AAAA '*) printf '%s\n' ${KODEX_TEST_DNS_AAAA_ADDRESSES:-} ;;
  *' A '*) printf '%s\n' ${KODEX_TEST_DNS_A_ADDRESSES:-} ;;
  *) exit 20 ;;
esac
BASH
chmod +x "$fake_bin/dig"

cat >"$fake_bin/curl" <<'BASH'
#!/usr/bin/env bash
set -euo pipefail
resolve=""
while (($# > 0)); do
  if [[ "$1" == --resolve ]]; then
    resolve=${2:-}
    break
  fi
  shift
done
[[ -n "$resolve" ]] || exit 21
printf '%s\n' "$resolve" >>"$KODEX_TEST_HTTP_LOG"
[[ -z "${KODEX_TEST_HTTP_FAIL_ADDRESS:-}" || "$resolve" != *"${KODEX_TEST_HTTP_FAIL_ADDRESS}"* ]] || exit 7
printf '%s' "${KODEX_TEST_HTTP_STATUS:-404}"
BASH
chmod +x "$fake_bin/curl"

export PATH="$fake_bin:$PATH"
export KUBECTL_LOG=$kubectl_log
export KODEX_TEST_BINDING_REMOVED="$temporary_directory/binding-removed"
export KODEX_TEST_PUBLIC_CERT_REMOVED="$temporary_directory/public-certificate-removed"
export KODEX_TEST_PUBLIC_DESCENDANT_OBSERVED="$temporary_directory/public-descendant-observed"
export KODEX_TEST_DNS_LOG="$temporary_directory/dns.log"
export KODEX_TEST_HTTP_LOG="$temporary_directory/http.log"
export KODEX_TEST_DNS_A_ADDRESSES=203.0.113.10
export KODEX_TEST_DNS_AAAA_ADDRESSES=
export KODEX_PUBLIC_TLS_ALLOWED_IPV4_ADDRESSES=203.0.113.10
export KODEX_PUBLIC_TLS_ALLOWED_IPV6_ADDRESSES=

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
rm -f "$KODEX_TEST_BINDING_REMOVED"
KODEX_TEST_PARAMETER_PRESENT=true KODEX_TEST_BINDING_PRESENT=true \
  "$repository_root/tools/install/deploy-platform.sh" \
    --context kodex-test --mode prepare-preflight --render "$render_file" \
    --public-tls-mode deferred >/dev/null
! grep -q ' args=.* delete ' "$kubectl_log" ||
  fail 'preflight preparation removed an active binding'

: >"$kubectl_log"
rm -f "$KODEX_TEST_BINDING_REMOVED"
if KODEX_TEST_PARAMETER_PRESENT=false KODEX_TEST_BINDING_PRESENT=true \
  KODEX_TEST_BINDING_MISMATCH=true \
  "$repository_root/tools/install/deploy-platform.sh" \
    --context kodex-test --mode prepare-preflight --render "$render_file" \
    --public-tls-mode deferred >/dev/null 2>&1; then
  fail 'preflight preparation accepted an unknown binding'
fi
! grep -q ' args=.* delete ' "$kubectl_log" ||
  fail 'preflight preparation removed an unknown binding'

: >"$kubectl_log"
rm -f "$KODEX_TEST_BINDING_REMOVED"
KODEX_TEST_PARAMETER_PRESENT=false KODEX_TEST_BINDING_PRESENT=true \
  KODEX_TEST_BINDING_MISMATCH=false \
  "$repository_root/tools/install/deploy-platform.sh" \
    --context kodex-test --mode prepare-preflight --render "$render_file" \
    --public-tls-mode deferred >/dev/null
[[ "$(grep -c ' delete validatingadmissionpolicybinding.admissionregistration.k8s.io/kodex-image-admission-controller-jobs ' \
  "$kubectl_log")" == 1 ]] ||
  fail 'preflight preparation did not retire the exact stale binding'
[[ -e "$KODEX_TEST_BINDING_REMOVED" ]] ||
  fail 'stale binding deletion did not complete'

: >"$kubectl_log"
rm -f "$KODEX_TEST_PUBLIC_CERT_REMOVED"
KODEX_TEST_PUBLIC_CERT_PRESENT=true \
  "$repository_root/tools/install/deploy-platform.sh" \
  --context kodex-test --mode defer-public-tls --render "$render_file" \
  --public-tls-mode deferred >/dev/null
grep -q ' delete certificate/staff-control-center-public ' "$kubectl_log" ||
  fail 'deferred mode did not retire the public Certificate'
! grep -q ' args=.* apply ' "$kubectl_log" ||
  fail 'deferred mode applied release resources'

: >"$kubectl_log"
: >"$KODEX_TEST_DNS_LOG"
: >"$KODEX_TEST_HTTP_LOG"
KODEX_TEST_DNS_AAAA_ADDRESSES=2001:db8::10 \
KODEX_PUBLIC_TLS_ALLOWED_IPV6_ADDRESSES=2001:db8::10 \
  "$repository_root/tools/install/deploy-platform.sh" \
    --context kodex-test --mode preflight --render "$render_file" \
    --public-tls-mode enabled >/dev/null
[[ "$(wc -l <"$KODEX_TEST_HTTP_LOG")" == 4 ]] ||
  fail 'dual-stack preflight did not probe every SAN/address pair'

: >"$kubectl_log"
if KODEX_TEST_DNS_AAAA_ADDRESSES=2001:db8::99 \
  KODEX_PUBLIC_TLS_ALLOWED_IPV6_ADDRESSES= \
  "$repository_root/tools/install/deploy-platform.sh" \
    --context kodex-test --mode preflight --render "$render_file" \
    --public-tls-mode enabled >/dev/null 2>&1; then
  fail 'preflight accepted an unauthorized AAAA address'
fi
! grep -q ' args=.* apply ' "$kubectl_log" ||
  fail 'unauthorized AAAA reached Kubernetes apply'

: >"$kubectl_log"
if KODEX_PUBLIC_TLS_ALLOWED_IPV4_ADDRESSES=203.0.113.10,198.51.100.20 \
  "$repository_root/tools/install/deploy-platform.sh" \
    --context kodex-test --mode preflight --render "$render_file" \
    --public-tls-mode enabled >/dev/null 2>&1; then
  fail 'preflight accepted an unused allowed IPv4 address'
fi
! grep -q ' args=.* apply ' "$kubectl_log" ||
  fail 'an inexact IPv4 allowlist reached Kubernetes apply'

: >"$kubectl_log"
if KODEX_TEST_HTTP_FAIL_ADDRESS=203.0.113.10 \
  "$repository_root/tools/install/deploy-platform.sh" \
    --context kodex-test --mode preflight --render "$render_file" \
    --public-tls-mode enabled >/dev/null 2>&1; then
  fail 'preflight accepted an unreachable HTTP-01 endpoint'
fi
! grep -q ' args=.* apply ' "$kubectl_log" ||
  fail 'unreachable HTTP-01 endpoint reached Kubernetes apply'

: >"$kubectl_log"
if KODEX_TEST_CLUSTER_ISSUER_READY=false \
  "$repository_root/tools/install/deploy-platform.sh" \
    --context kodex-test --mode preflight --render "$render_file" \
    --public-tls-mode enabled >/dev/null 2>&1; then
  fail 'preflight accepted a non-ready ClusterIssuer'
fi
! grep -q ' args=.* apply ' "$kubectl_log" ||
  fail 'non-ready ClusterIssuer reached Kubernetes apply'

: >"$kubectl_log"
rm -f "$KODEX_TEST_PUBLIC_CERT_REMOVED"
rm -f "$KODEX_TEST_PUBLIC_DESCENDANT_OBSERVED"
if KODEX_TEST_PUBLIC_CERT_PRESENT=true KODEX_TEST_PUBLIC_CERT_READY=false \
  KODEX_TEST_PUBLIC_DESCENDANT_ONCE=true \
  "$repository_root/tools/install/deploy-platform.sh" \
    --context kodex-test --mode apply --render "$render_file" \
    --public-tls-mode enabled >/dev/null 2>&1; then
  fail 'apply accepted a public Certificate readiness failure'
fi
grep -q ' delete certificate/staff-control-center-public ' "$kubectl_log" ||
  fail 'readiness failure did not retire the owner-managed public Certificate'
[[ -e "$KODEX_TEST_PUBLIC_CERT_REMOVED" ]] ||
  fail 'owner-managed public Certificate deletion did not complete'
[[ -e "$KODEX_TEST_PUBLIC_DESCENDANT_OBSERVED" ]] ||
  fail 'readiness cleanup did not wait for public TLS descendants'

: >"$kubectl_log"
rm -f "$KODEX_TEST_PUBLIC_CERT_REMOVED"
if KODEX_TEST_PUBLIC_CERT_PRESENT=true KODEX_TEST_PUBLIC_CERT_READY=false \
  KODEX_TEST_PUBLIC_CERT_OWNER=false \
  "$repository_root/tools/install/deploy-platform.sh" \
    --context kodex-test --mode apply --render "$render_file" \
    --public-tls-mode enabled >/dev/null 2>&1; then
  fail 'apply accepted an unmanaged public Certificate readiness failure'
fi
! grep -q ' delete certificate/staff-control-center-public ' "$kubectl_log" ||
  fail 'readiness cleanup deleted an unmanaged public Certificate'
[[ ! -e "$KODEX_TEST_PUBLIC_CERT_REMOVED" ]] ||
  fail 'unmanaged public Certificate deletion marker exists'

printf 'Deploy platform preflight test passed\n'
