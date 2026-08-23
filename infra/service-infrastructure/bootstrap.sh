#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Service infrastructure bootstrap failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 --context <exact-context> --mode preflight|apply-controllers|apply-vault|readback" >&2
}

expected_context=""
mode=""
while (($# > 0)); do
  case "$1" in
    --context) expected_context="${2:-}"; shift 2 ;;
    --mode) mode="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$expected_context" ]] || fail 'exact context is required'
[[ "$mode" == preflight || "$mode" == apply-controllers || "$mode" == apply-vault || "$mode" == readback ]] ||
  fail 'mode is invalid'

for command_name in helm jq kubectl sha256sum; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ "$(kubectl config current-context)" == "$expected_context" ]] || fail 'current Kubernetes context mismatch'

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
lock_file="$script_directory/charts.lock.json"
jq -e '
  .schemaVersion == 1 and (.charts | length) == 4 and
  ([.charts[].name] | unique | length) == 4 and
  all(.charts[];
    (.name | test("^[a-z0-9-]+$")) and
    (.chart | test("^[a-z0-9-]+$")) and
    (.version | test("^v?[0-9]+\\.[0-9]+\\.[0-9]+$")) and
    (.sha256 | test("^[a-f0-9]{64}$")))
' "$lock_file" >/dev/null || fail 'chart lock is invalid'

kubectl get namespace cert-manager >/dev/null 2>&1 || fail 'cert-manager namespace is absent'
kubectl -n cert-manager wait --for=condition=Available deployment/cert-manager --timeout=180s >/dev/null ||
  fail 'cert-manager is unavailable'

if [[ "$mode" == preflight ]]; then
  printf 'Service infrastructure preflight completed\n'
  exit 0
fi

download_directory=$(mktemp -d)
trap 'rm -rf -- "$download_directory"' EXIT

download_chart() {
  local name=$1
  local chart repository version expected_sha chart_directory archive
  chart=$(jq -er --arg name "$name" '.charts[] | select(.name == $name) | .chart' "$lock_file")
  repository=$(jq -er --arg name "$name" '.charts[] | select(.name == $name) | .repository' "$lock_file")
  version=$(jq -er --arg name "$name" '.charts[] | select(.name == $name) | .version' "$lock_file")
  expected_sha=$(jq -er --arg name "$name" '.charts[] | select(.name == $name) | .sha256' "$lock_file")
  chart_directory="$download_directory/$name"
  mkdir -p "$chart_directory"
  if [[ "$repository" == oci://* ]]; then
    helm pull "$repository" --version "$version" --destination "$chart_directory" >/dev/null
  else
    helm repo add "mattercodex-$name" "$repository" --force-update >/dev/null
    helm pull "mattercodex-$name/$chart" --version "$version" --destination "$chart_directory"
  fi
  archive=$(find "$chart_directory" -maxdepth 1 -type f -name '*.tgz' -print -quit)
  [[ -n "$archive" ]] || fail "chart archive is absent: $name"
  printf '%s  %s\n' "$expected_sha" "$archive" | sha256sum --check --status ||
    fail "chart digest mismatch: $name"
  printf '%s\n' "$archive"
}

require_ready_deployment_by_selector() {
  local namespace=$1
  local selector=$2
  local description=$3
  local deployment_list deployment_name

  deployment_list=$(kubectl -n "$namespace" get deployment -l "$selector" -o json) ||
    fail "$description deployment query failed"
  deployment_name=$(jq -er '
    if (.items | length) == 1 then
      .items[0].metadata.name
    else
      error("expected exactly one deployment")
    end
  ' <<<"$deployment_list") || fail "$description deployment is not unique"

  kubectl -n "$namespace" rollout status "deployment/$deployment_name" --timeout=180s >/dev/null ||
    fail "$description deployment rollout is incomplete"
  kubectl -n "$namespace" get deployment "$deployment_name" -o json | jq -e '
    (.spec.replicas // 0) > 0 and
    .status.observedGeneration == .metadata.generation and
    (.status.updatedReplicas // 0) == .spec.replicas and
    (.status.readyReplicas // 0) == .spec.replicas and
    (.status.availableReplicas // 0) == .spec.replicas
  ' >/dev/null || fail "$description deployment is not fully ready"
}

require_secrets_store_csi_fsgroup_policy() {
  [[ $(kubectl get csidriver secrets-store.csi.k8s.io -o jsonpath='{.spec.fsGroupPolicy}') == File ]] ||
    fail 'Secrets Store CSI Driver fsGroup policy is not File'
}

reconcile_secrets_store_csi_fsgroup_policy() {
  kubectl patch csidriver secrets-store.csi.k8s.io --type=merge \
    --patch '{"spec":{"fsGroupPolicy":"File"}}' >/dev/null ||
    fail 'Secrets Store CSI Driver fsGroup policy reconciliation failed'
  require_secrets_store_csi_fsgroup_policy
}

require_vault_csi_provider() {
  local daemonset_json

  kubectl -n mattercodex-system rollout status daemonset/vault-csi-provider --timeout=180s >/dev/null ||
    fail 'Vault CSI provider rollout is incomplete'
  daemonset_json=$(kubectl -n mattercodex-system get daemonset vault-csi-provider -o json) ||
    fail 'Vault CSI provider readback failed'
  jq -e '
    .status.observedGeneration == .metadata.generation and
    (.status.desiredNumberScheduled // 0) > 0 and
    .status.updatedNumberScheduled == .status.desiredNumberScheduled and
    .status.numberReady == .status.desiredNumberScheduled and
    (.spec.template.spec.containers | length) == 1 and
    .spec.template.spec.containers[0].name == "vault-csi-provider" and
    (.spec.template.spec.containers[0].args | index("--vault-addr=https://vault.mattercodex-system.svc:8200")) != null and
    (.spec.template.spec.containers[0].args | index("--vault-tls-ca-cert=/vault/tls/ca.crt")) != null and
    (.spec.template.spec.containers[0].args | index("--vault-tls-server-name=vault.mattercodex-system.svc.cluster.local")) != null and
    ([.spec.template.spec.containers[0].args[] | select(test("vault-tls-skip-verify"))] | length) == 0 and
    any(.spec.template.spec.containers[0].volumeMounts[];
      .name == "vault-server-ca" and
      .mountPath == "/vault/tls" and
      .readOnly == true) and
    any(.spec.template.spec.volumes[];
      .name == "vault-server-ca" and
      .secret.secretName == "mattercodex-vault-server-tls" and
      .secret.items == [{"key":"ca.crt","path":"ca.crt"}])
  ' <<<"$daemonset_json" >/dev/null || fail 'Vault CSI provider TLS contract mismatch'
}

if [[ "$mode" == apply-controllers ]]; then
  kubectl create namespace mattercodex-trust --dry-run=client -o yaml | kubectl apply -f - >/dev/null

  csi_chart=$(download_chart secrets-store-csi-driver)
  helm upgrade --install mattercodex-secrets-store-csi "$csi_chart" \
    --namespace kube-system --values "$script_directory/secrets-store-csi-values.yaml" \
    --atomic --wait --timeout 10m
  reconcile_secrets_store_csi_fsgroup_policy

  trust_chart=$(download_chart trust-manager)
  helm upgrade --install mattercodex-trust-manager "$trust_chart" \
    --namespace cert-manager --values "$script_directory/trust-manager-values.yaml" \
    --atomic --wait --timeout 10m

  vso_chart=$(download_chart vault-secrets-operator)
  helm upgrade --install mattercodex-vault-secrets-operator "$vso_chart" \
    --namespace vault-secrets-operator-system --create-namespace \
    --values "$script_directory/vault-secrets-operator-values.yaml" \
    --atomic --wait --timeout 10m
fi

if [[ "$mode" == apply-vault ]]; then
  kubectl get namespace mattercodex-system >/dev/null 2>&1 || fail 'MatterCodex namespace is absent'
  kubectl -n mattercodex-system get secret mattercodex-vault-server-tls >/dev/null 2>&1 ||
    fail 'Vault server TLS secret is absent'
  vault_chart=$(download_chart vault)
  helm upgrade --install mattercodex-vault "$vault_chart" \
    --namespace mattercodex-system --values "$script_directory/vault-values.yaml" \
    --atomic --wait --timeout 10m
  require_vault_csi_provider
fi

if [[ "$mode" == readback ]]; then
  for resource in \
    secretproviderclasses.secrets-store.csi.x-k8s.io \
    bundles.trust.cert-manager.io \
    vaultauths.secrets.hashicorp.com \
    vaultconnections.secrets.hashicorp.com \
    vaultstaticsecrets.secrets.hashicorp.com; do
    kubectl get customresourcedefinition "$resource" >/dev/null 2>&1 ||
      fail "required CRD is absent: $resource"
  done
  kubectl -n kube-system rollout status \
    daemonset/mattercodex-secrets-store-csi-secrets-store-csi-driver --timeout=180s >/dev/null ||
    fail 'Secrets Store CSI Driver rollout is incomplete'
  require_secrets_store_csi_fsgroup_policy
  require_ready_deployment_by_selector cert-manager \
    'app.kubernetes.io/instance=mattercodex-trust-manager,app.kubernetes.io/name=trust-manager' \
    'trust-manager'
  require_ready_deployment_by_selector vault-secrets-operator-system \
    'app.kubernetes.io/instance=mattercodex-vault-secrets-operator,app.kubernetes.io/name=vault-secrets-operator,app.kubernetes.io/component=controller-manager' \
    'Vault Secrets Operator'
  if kubectl get namespace mattercodex-system >/dev/null 2>&1; then
    kubectl -n mattercodex-system get statefulset vault >/dev/null
    kubectl -n mattercodex-system get service vault >/dev/null
    require_vault_csi_provider
  fi
fi

printf 'Service infrastructure bootstrap completed: %s\n' "$mode"
