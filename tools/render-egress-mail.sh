#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 5 ]]; then
  echo "usage: render-egress-mail.sh staging|production image-digest registry-fqdn mailbox-configuration trusted-resolv-conf" >&2
  exit 2
fi

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
configuration=$(realpath -e -- "$4")
resolv_conf=$(realpath -e -- "$5")
temporary=$(mktemp -d)
trap 'rm -rf -- "$temporary"' EXIT

"$root/tools/render-egress-gateway.sh" "$1" "$2" "$3" >"$temporary/base.yaml"
# Сначала удаляем только прежнюю mail-проекцию; общие/STT resources сохраняются.
yq 'select((.kind != "ConfigMap" or .data."mail-policy.json" == null) and
  (.kind != "NetworkPolicy" or .metadata.name != "egress-gateway-mail-destinations"))' \
  "$temporary/base.yaml" >"$temporary/runtime.yaml"

revision=$(jq -r '.metadata.revision' "$root/deploy/k8s/base/egress-gateway/policy.json")
digest=$(yq -r '.spec.template.spec.containers[0].env[] |
  select(.name == "EGRESS_GATEWAY_EXPECTED_POLICY_DIGEST") | .value' \
  "$root/deploy/k8s/base/egress-gateway/deployment.yaml")
(
  cd -- "$root/services/external/egress-gateway"
  go run ./cmd/mail-projection -configuration "$configuration" \
    -policy "$root/deploy/k8s/base/egress-gateway/policy.json" \
    -revision "$revision" -digest "$digest" -resolv-conf "$resolv_conf" \
    -output "$temporary/mail"
)
yq -i '.metadata.namespace = "kodex-system"' "$temporary/mail/mail-deployment-patch.json"
jq -n '{apiVersion:"kustomize.config.k8s.io/v1beta1",kind:"Kustomization",
  resources:["runtime.yaml","mail/mail-configmap.json","mail/mail-networkpolicy.json"],
  patches:[{path:"mail/mail-deployment-patch.json"}]}' >"$temporary/kustomization.yaml"
kubectl kustomize "$temporary"
