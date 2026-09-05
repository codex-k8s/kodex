#!/usr/bin/env bash
set -euo pipefail
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
temporary=$(mktemp -d)
trap 'rm -rf -- "$temporary"' EXIT
for profile in web-only web-with-mattermost; do
  kubectl kustomize "$root/deploy/k8s/profiles/$profile" >"$temporary/render.yaml"
  yq -o=json -I=0 '.' "$temporary/render.yaml" | jq -s -e '
    any(.[]; .kind == "ConfigMap" and .metadata.name == "integration-gateway-runtime" and
      .data.INTEGRATION_GATEWAY_CLAIM_LIMIT == "1" and .data.INTEGRATION_GATEWAY_OPERATION_TIMEOUT == "20s") and
    any(.[]; .kind == "Deployment" and .metadata.name == "integration-gateway" and
      .spec.template.spec.automountServiceAccountToken == false and
      .spec.template.spec.serviceAccountName == "integration-gateway" and
      any(.spec.template.spec.containers[]; .name == "integration-gateway" and
        .readinessProbe.httpGet.path == "/readyz" and .livenessProbe.httpGet.path == "/healthz")) and
    any(.[]; .kind == "NetworkPolicy" and .metadata.name == "integration-gateway-exact-runtime-paths" and
      all(.spec.egress[]; (.to | length) > 0) and
      any(.spec.egress[];
        .ports == [{"protocol":"TCP","port":8443}] and
        .to == [{"namespaceSelector":{"matchLabels":{"kubernetes.io/metadata.name":"kodex-system"}},
          "podSelector":{"matchLabels":{"app.kubernetes.io/name":"email-bridge"}}}])) and
    any(.[]; .kind == "Service" and .metadata.name == "email-bridge" and
      any(.spec.ports[]; .name == "https" and .port == 443 and .targetPort == "https")) and
    any(.[]; .kind == "Deployment" and .metadata.name == "email-bridge" and
      any(.spec.template.spec.containers[]; .name == "email-bridge" and
        any(.ports[]; .name == "https" and .containerPort == 8443))) and
    any(.[]; .kind == "PrometheusRule" and .metadata.name == "integration-gateway" and
      any(.spec.groups[].rules[]; .alert == "IntegrationGatewayUnknownOutcome" and
        (.annotations.runbook_url | startswith("https://"))))
  ' >/dev/null
  printf 'Integration gateway render passed: %s\n' "$profile"
done
