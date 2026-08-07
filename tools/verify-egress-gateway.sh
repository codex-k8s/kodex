#!/usr/bin/env bash
set -euo pipefail

repository_root="$(git rev-parse --show-toplevel)"
temporary_directory="$(mktemp -d)"
trap 'rm -rf "${temporary_directory}"' EXIT

gateway_render="${temporary_directory}/egress-gateway.yaml"
consumer_render="${temporary_directory}/integration-gateway.yaml"

kubectl kustomize "${repository_root}/deploy/k8s/base/egress-gateway" >"${gateway_render}"
kubectl kustomize "${repository_root}/deploy/k8s/base/integration-gateway" >"${consumer_render}"

jq -e . "${repository_root}/contracts/egress/v1/egress-gateway-policy.schema.json" >/dev/null
jq -e . "${repository_root}/deploy/k8s/base/egress-gateway/policy.json" >/dev/null
yq -e '.packages[] | select(.id == "egress-gateway-policy-v1" and .format == "json-schema" and
  .owner == "egress-gateway" and .source == "contracts/egress/v1/egress-gateway-policy.schema.json")' \
  "${repository_root}/contracts/registry.yaml" >/dev/null

yq -e 'select(.kind == "Deployment" and .metadata.name == "egress-gateway") |
  .spec.replicas == 2 and
  .spec.revisionHistoryLimit == 2 and
  .spec.strategy.rollingUpdate.maxUnavailable == 0 and
  .spec.template.spec.automountServiceAccountToken == false and
  .spec.template.spec.hostNetwork == false and
  .spec.template.spec.dnsPolicy == "ClusterFirst" and
  .spec.template.spec.enableServiceLinks == false and
  .spec.template.spec.containers[0].startupProbe.httpGet.path == "/livez" and
  .spec.template.spec.containers[0].readinessProbe.httpGet.path == "/readyz" and
  .spec.template.spec.containers[0].livenessProbe.httpGet.path == "/livez" and
  .spec.template.spec.containers[0].securityContext.allowPrivilegeEscalation == false and
  .spec.template.spec.containers[0].securityContext.readOnlyRootFilesystem == true and
  .spec.template.spec.containers[0].securityContext.privileged != true' "${gateway_render}" >/dev/null

yq -e 'select(.kind == "ServiceAccount" and .metadata.name == "egress-gateway") |
  .automountServiceAccountToken == false' "${gateway_render}" >/dev/null

yq -e 'select(.kind == "ConfigMap" and .metadata.name == "egress-gateway-policy") |
  .immutable == true and has("data") and .data."policy.json" != ""' "${gateway_render}" >/dev/null

yq -e 'select(.kind == "Service" and .metadata.name == "egress-gateway") |
  .spec.selector."app.kubernetes.io/name" == "egress-gateway" and
  .spec.selector."app.kubernetes.io/component" == "platform-egress" and
  ([.spec.ports[] | select(.name == "connect" and .port == 8080)] | length == 1)' "${gateway_render}" >/dev/null

yq -e 'select(.kind == "Service" and .metadata.name == "egress-gateway-technical") |
  .spec.publishNotReadyAddresses == true and
  .spec.selector."app.kubernetes.io/name" == "egress-gateway" and
  .spec.selector."app.kubernetes.io/component" == "platform-egress" and
  ([.spec.ports[] | select(.name == "metrics" and .port == 9090)] | length == 1)' "${gateway_render}" >/dev/null

yq -e 'select(.kind == "NetworkPolicy" and .metadata.name == "egress-gateway-exact-runtime-paths") |
  .spec.ingress[] | select(.ports[].protocol == "TCP" and .ports[].port == 8080) |
  .from[] | select(.namespaceSelector.matchLabels."kubernetes.io/metadata.name" == "mattercodex-system" and
    .podSelector.matchLabels."app.kubernetes.io/name" == "integration-gateway" and
    .podSelector.matchLabels."app.kubernetes.io/component" == "external-gateway")' "${gateway_render}" >/dev/null

yq -e 'select(.kind == "NetworkPolicy" and .metadata.name == "egress-gateway-exact-runtime-paths") |
  .spec.ingress[] | select(.ports[].protocol == "TCP" and .ports[].port == 9090) |
  .from[] | select(.namespaceSelector.matchLabels."kubernetes.io/metadata.name" == "monitoring" and
    .podSelector.matchLabels."app.kubernetes.io/name" == "prometheus")' "${gateway_render}" >/dev/null

yq -e 'select(.kind == "NetworkPolicy" and .metadata.name == "egress-gateway-exact-runtime-paths") |
  .spec.egress[] | select(.ports[].port == 53) |
  .to[] | select(.namespaceSelector.matchLabels."kubernetes.io/metadata.name" == "kube-system" and
    .podSelector.matchLabels."k8s-app" == "kube-dns")' "${gateway_render}" >/dev/null

yq -e 'select(.kind == "NetworkPolicy" and .metadata.name == "egress-gateway-exact-runtime-paths") |
  .spec.egress[] | select(.to == null and .ports[].protocol == "TCP" and .ports[].port == 443)' "${gateway_render}" >/dev/null

yq -e 'select(.kind == "NetworkPolicy" and .metadata.name == "egress-gateway-exact-runtime-paths") |
  (.spec.ingress | length == 2) and (.spec.egress | length == 2)' "${gateway_render}" >/dev/null

yq -e 'select(.kind == "NetworkPolicy" and .metadata.name == "integration-gateway-exact-runtime-paths") |
  .spec.egress[] | select(.ports[].protocol == "TCP" and .ports[].port == 8080) |
  .to[] | select(.namespaceSelector.matchLabels."kubernetes.io/metadata.name" == "mattercodex-system" and
    .podSelector.matchLabels."app.kubernetes.io/name" == "egress-gateway" and
    .podSelector.matchLabels."app.kubernetes.io/component" == "platform-egress")' "${consumer_render}" >/dev/null

if yq -e 'select(.kind == "NetworkPolicy" and .metadata.name == "integration-gateway-exact-runtime-paths") |
  .spec.egress[] | select(.to == null and .ports[].protocol == "TCP" and .ports[].port == 443)' "${consumer_render}" >/dev/null 2>&1; then
  echo "Integration gateway render contains direct destination-less external HTTPS egress." >&2
  exit 1
fi

if rg -n '0\.0\.0\.0/0|::/0|privileged:[[:space:]]*true|hostNetwork:[[:space:]]*true' "${gateway_render}" "${consumer_render}" >/dev/null; then
  echo "Rendered egress gateway boundary contains a prohibited broad or privileged setting." >&2
  exit 1
fi

while IFS= read -r runbook_url; do
  if [[ ! "${runbook_url}" =~ ^https:// ]]; then
    echo "Prometheus alert contains a non-HTTPS runbook URL." >&2
    exit 1
  fi
done < <(yq -r 'select(.kind == "PrometheusRule" and .metadata.name == "egress-gateway") |
  .spec.groups[].rules[].annotations.runbook_url' "${gateway_render}")

yq -r 'select(.kind == "ConfigMap" and .metadata.name == "egress-gateway-dashboard") |
  .data."egress-gateway.json"' "${gateway_render}" | jq -e . >/dev/null

policy_digest="$(jq -cS . "${repository_root}/deploy/k8s/base/egress-gateway/policy.json" | tr -d '\n' | sha256sum | cut -d' ' -f1)"
expected_digest="$(yq -r 'select(.kind == "Deployment" and .metadata.name == "egress-gateway") |
  .spec.template.spec.containers[0].env[] | select(.name == "EGRESS_GATEWAY_EXPECTED_POLICY_DIGEST") | .value' "${gateway_render}")"
if [[ "${policy_digest}" != "${expected_digest}" ]]; then
  echo "Rendered expected policy digest does not match canonical policy content." >&2
  exit 1
fi

policy_revision="$(jq -r '.metadata.revision' "${repository_root}/deploy/k8s/base/egress-gateway/policy.json")"
expected_revision="$(yq -r 'select(.kind == "Deployment" and .metadata.name == "egress-gateway") |
  .spec.template.spec.containers[0].env[] | select(.name == "EGRESS_GATEWAY_EXPECTED_POLICY_REVISION") | .value' "${gateway_render}")"
if [[ "${policy_revision}" != "${expected_revision}" ]]; then
  echo "Rendered expected policy revision does not match immutable policy content." >&2
  exit 1
fi

echo "Egress gateway render and network boundary verification passed."
