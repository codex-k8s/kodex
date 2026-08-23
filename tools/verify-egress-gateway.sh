#!/usr/bin/env bash
set -euo pipefail

repository_root="$(git rev-parse --show-toplevel)"
temporary_directory="$(mktemp -d)"
trap 'rm -rf -- "$temporary_directory"' EXIT

base_render="$temporary_directory/base.yaml"
staging_render="$temporary_directory/staging.yaml"
production_render="$temporary_directory/production.yaml"
consumer_render="$temporary_directory/integration-gateway.yaml"
synthetic_digest="sha256:$(printf '1%.0s' {1..64})"
synthetic_registry="registry-pull.example.com"
renderer="$repository_root/tools/render-egress-gateway.sh"

expect_renderer_rejection() {
  local registry_host=$1
  if "$renderer" staging "$synthetic_digest" "$registry_host" >/dev/null 2>&1; then
    echo "renderer accepted an invalid registry DNS name" >&2
    exit 1
  fi
}

kubectl kustomize "$repository_root/deploy/k8s/base/egress-gateway" >"$base_render"
"$renderer" staging "$synthetic_digest" "$synthetic_registry" >"$staging_render"
"$renderer" production "$synthetic_digest" "$synthetic_registry" >"$production_render"
kubectl kustomize "$repository_root/deploy/k8s/base/integration-gateway" >"$consumer_render"

label_61="$(printf 'a%.0s' {1..61})"
label_63="$(printf 'a%.0s' {1..63})"
label_64="$(printf 'a%.0s' {1..64})"
"$renderer" staging "$synthetic_digest" "$label_63.$label_63.$label_63.$label_61" >/dev/null
expect_renderer_rejection "registry..example.com"
expect_renderer_rejection "registry.-bad.example.com"
expect_renderer_rejection "registry.bad-.example.com"
expect_renderer_rejection "registry.example.com."
expect_renderer_rejection "$label_64.example.com"
expect_renderer_rejection "$label_63.$label_63.$label_63.$(printf 'a%.0s' {1..62})"

jq -e . "$repository_root/contracts/egress/v1/egress-gateway-policy.schema.json" >/dev/null
jq -e . "$repository_root/deploy/k8s/base/egress-gateway/policy.json" >/dev/null
yq -e '.packages[] | select(.id == "egress-gateway-policy-v1" and .format == "json-schema" and
  .owner == "egress-gateway" and .source == "contracts/egress/v1/egress-gateway-policy.schema.json")' \
  "$repository_root/contracts/registry.yaml" >/dev/null

for gateway_render in "$base_render" "$staging_render" "$production_render"; do
  yq -e 'select(.kind == "Deployment" and .metadata.name == "egress-gateway") |
    .spec.replicas == 2 and
    .spec.revisionHistoryLimit == 2 and
    .spec.strategy.rollingUpdate.maxUnavailable == 0 and
    .spec.template.spec.automountServiceAccountToken == false and
    .spec.template.spec.hostNetwork == false and
    .spec.template.spec.dnsPolicy == "ClusterFirst" and
    .spec.template.spec.enableServiceLinks == false and
    .spec.template.spec.terminationGracePeriodSeconds == 45 and
    .spec.template.spec.containers[0].startupProbe.httpGet.path == "/healthz" and
    .spec.template.spec.containers[0].readinessProbe.httpGet.path == "/readyz" and
    .spec.template.spec.containers[0].livenessProbe.httpGet.path == "/healthz" and
    .spec.template.spec.containers[0].securityContext.allowPrivilegeEscalation == false and
    .spec.template.spec.containers[0].securityContext.readOnlyRootFilesystem == true and
    .spec.template.spec.containers[0].securityContext.privileged != true and
    ([.spec.template.spec.volumes[] | select(.secret != null)] | length == 0)' "$gateway_render" >/dev/null

  yq -e 'select(.kind == "ServiceAccount" and .metadata.name == "egress-gateway") |
    .automountServiceAccountToken == false and
    (.imagePullSecrets | length) == 1 and
    .imagePullSecrets[0].name == "mattercodex-image-pull"' "$gateway_render" >/dev/null

  policy_name="$(yq -r 'select(.kind == "ConfigMap" and .immutable == true and .data."policy.json" != "") | .metadata.name' "$gateway_render")"
  if [[ ! "$policy_name" =~ ^egress-gateway-policy-[a-z0-9]+$ ]]; then
    echo "rendered immutable policy is not content-addressed" >&2
    exit 1
  fi
  deployment_policy_name="$(yq -r 'select(.kind == "Deployment" and .metadata.name == "egress-gateway") |
    .spec.template.spec.volumes[] | select(.name == "policy") | .configMap.name' "$gateway_render")"
  if [[ "$deployment_policy_name" != "$policy_name" ]]; then
    echo "Deployment does not reference the exact content-addressed policy" >&2
    exit 1
  fi

  yq -e 'select(.kind == "Service" and .metadata.name == "egress-gateway") |
    .spec.selector."app.kubernetes.io/name" == "egress-gateway" and
    .spec.selector."app.kubernetes.io/component" == "platform-egress" and
    ([.spec.ports[] | select(.name == "connect" and .port == 8080)] | length == 1)' "$gateway_render" >/dev/null

  yq -e 'select(.kind == "Service" and .metadata.name == "egress-gateway-technical") |
    .spec.publishNotReadyAddresses == true and
    ([.spec.ports[] | select(.name == "metrics" and .port == 9090)] | length == 1)' "$gateway_render" >/dev/null

  yq -e 'select(.kind == "NetworkPolicy" and .metadata.name == "egress-gateway-exact-runtime-paths") |
    (.spec.ingress | length == 2) and (.spec.egress | length == 2) and
    ([.spec.ingress[] | select(.ports[].port == 8080) | .from[] |
      select(.namespaceSelector.matchLabels."kubernetes.io/metadata.name" == "mattercodex-system" and
        .podSelector.matchLabels."app.kubernetes.io/name" == "integration-gateway" and
        .podSelector.matchLabels."app.kubernetes.io/component" == "integration-worker")] | length == 1) and
    ([.spec.ingress[] | select(.ports[].port == 8080) | .from[] |
      select(.namespaceSelector.matchLabels."kubernetes.io/metadata.name" == "mattercodex-system" and
        .podSelector.matchLabels."app.kubernetes.io/name" == "interaction-gateway" and
        .podSelector.matchLabels."app.kubernetes.io/component" == "interaction-adapter")] | length == 1) and
    ([.spec.ingress[] | select(.ports[].port == 8080) | .from[] |
      select(.namespaceSelector.matchLabels."kubernetes.io/metadata.name" == "mattercodex-system" and
        .podSelector.matchLabels."app.kubernetes.io/name" == "agent-runner" and
        .podSelector.matchLabels."app.kubernetes.io/component" == "role-runtime" and
        .podSelector.matchLabels."runtime.mattercodex.dev/managed" == "true")] | length == 1) and
    ([.spec.ingress[] | select(.ports[].port == 8080) | .from[] |
      select(.namespaceSelector.matchLabels."kubernetes.io/metadata.name" == "mattercodex-system" and
        .podSelector.matchLabels."app.kubernetes.io/name" == "release-artifact-materializer" and
        .podSelector.matchLabels."app.kubernetes.io/component" == "release-bootstrap")] | length == 1) and
    ([.spec.ingress[] | select(.ports[].port == 9090) | .from[] |
      select(.namespaceSelector.matchLabels."kubernetes.io/metadata.name" == "monitoring" and
        .podSelector.matchLabels."app.kubernetes.io/name" == "prometheus")] | length == 1) and
    ([.spec.egress[] | select(.ports[].port == 53) | .to[] |
      select(.namespaceSelector.matchLabels."kubernetes.io/metadata.name" == "kube-system" and
        .podSelector.matchLabels."k8s-app" == "kube-dns")] | length == 1) and
    ([.spec.egress[] | select(.to == null and .ports[].protocol == "TCP" and .ports[].port == 443)] | length == 1)' \
    "$gateway_render" >/dev/null

  if rg -n '0\.0\.0\.0/0|::/0|privileged:[[:space:]]*true|hostNetwork:[[:space:]]*true' "$gateway_render" >/dev/null; then
    echo "rendered egress gateway contains a prohibited broad or privileged setting" >&2
    exit 1
  fi
  if yq -e 'select(.kind == "Role" or .kind == "RoleBinding" or .kind == "ClusterRole" or .kind == "ClusterRoleBinding")' \
    "$gateway_render" >/dev/null 2>&1; then
    echo "rendered egress gateway unexpectedly contains RBAC" >&2
    exit 1
  fi
done

for environment_name in staging production; do
  delivered_render="$temporary_directory/$environment_name.yaml"
  ENVIRONMENT_NAME="$environment_name" yq -e 'select(.kind == "Deployment" and .metadata.name == "egress-gateway") |
    .metadata.namespace == "mattercodex-system" and
    .spec.template.metadata.labels."mattercodex.dev/environment" == strenv(ENVIRONMENT_NAME) and
    .spec.template.spec.containers[0].image == "registry-pull.example.com/mattercodex/egress-gateway@sha256:1111111111111111111111111111111111111111111111111111111111111111"' \
    "$delivered_render" >/dev/null
  if rg -F '@sha256:0000000000000000000000000000000000000000000000000000000000000000' "$delivered_render" >/dev/null; then
    echo "environment render contains an unresolved image" >&2
    exit 1
  fi
done

yq -e 'select(.kind == "NetworkPolicy" and .metadata.name == "integration-gateway-exact-runtime-paths") |
  ([.spec.egress[] | select(.ports[].protocol == "TCP" and .ports[].port == 8080) | .to[] |
    select(.namespaceSelector.matchLabels."kubernetes.io/metadata.name" == "mattercodex-system" and
      .podSelector.matchLabels."app.kubernetes.io/name" == "egress-gateway" and
      .podSelector.matchLabels."app.kubernetes.io/component" == "platform-egress")] | length == 1) and
  ([.spec.egress[] | select(.ports[].port == 9090)] | length == 0) and
  ([.spec.egress[] | select(.to == null and .ports[].protocol == "TCP" and .ports[].port == 443)] | length == 0)' \
  "$consumer_render" >/dev/null

while IFS= read -r runbook_url; do
  if [[ ! "$runbook_url" =~ ^https:// ]]; then
    echo "Prometheus alert contains a non-HTTPS runbook URL" >&2
    exit 1
  fi
done < <(yq -r 'select(.kind == "PrometheusRule" and .metadata.name == "egress-gateway") |
  .spec.groups[].rules[].annotations.runbook_url' "$base_render")

yq -e 'select(.kind == "PrometheusRule" and .metadata.name == "egress-gateway") |
  ([.spec.groups[].rules[] | select(.alert == "EgressGatewayNotReady") |
    select(.expr | contains("max(") | not) | select(.expr | contains("sum(up")) |
    select(.expr | contains("or vector(0)"))] | length == 1) and
  ([.spec.groups[].rules[] | select(.alert == "EgressGatewayPolicyInactive") |
    select(.expr | contains("max(") | not) | select(.expr | contains("sum(up")) |
    select(.expr | contains("or vector(0)"))] | length == 1) and
  ([.spec.groups[].rules[] | select(.alert == "EgressGatewayConnectionLimitRejections") |
    select(.expr | contains("reason=\"connection_limit\""))] | length == 1)' "$base_render" >/dev/null

dashboard_json="$(yq -r 'select(.kind == "ConfigMap" and .metadata.name == "egress-gateway-dashboard") |
  .data."egress-gateway.json"' "$base_render")"
jq -e '([.panels[] | select(.title == "Not-ready scraped replicas")] | length == 1) and
  ([.panels[] | select(.title == "Not-ready scraped replicas") |
    .targets[] | select(.expr | contains("or vector(0)"))] | length == 1) and
  ([.panels[] | select(.title == "Connection limit rejections") |
    .targets[] | select(.expr | contains("connection_limit"))] | length == 1)' <<<"$dashboard_json" >/dev/null

policy_digest="$(cd "$repository_root/services/external/egress-gateway" && go run ./cmd/policy-digest "$repository_root/deploy/k8s/base/egress-gateway/policy.json")"
expected_digest="$(yq -r 'select(.kind == "Deployment" and .metadata.name == "egress-gateway") |
  .spec.template.spec.containers[0].env[] | select(.name == "EGRESS_GATEWAY_EXPECTED_POLICY_DIGEST") | .value' "$base_render")"
if [[ "$policy_digest" != "$expected_digest" ]]; then
  echo "rendered expected policy digest does not match runtime canonical policy digest" >&2
  exit 1
fi

policy_revision="$(jq -r '.metadata.revision' "$repository_root/deploy/k8s/base/egress-gateway/policy.json")"
expected_revision="$(yq -r 'select(.kind == "Deployment" and .metadata.name == "egress-gateway") |
  .spec.template.spec.containers[0].env[] | select(.name == "EGRESS_GATEWAY_EXPECTED_POLICY_REVISION") | .value' "$base_render")"
if [[ "$policy_revision" != "$expected_revision" ]]; then
  echo "rendered expected policy revision does not match immutable policy content" >&2
  exit 1
fi

schema_shutdown_maximum="$(jq -r '.properties.spec.properties.limits.properties.shutdownTimeoutMilliseconds.maximum' \
  "$repository_root/contracts/egress/v1/egress-gateway-policy.schema.json")"
termination_grace="$(yq -r 'select(.kind == "Deployment" and .metadata.name == "egress-gateway") |
  .spec.template.spec.terminationGracePeriodSeconds' "$base_render")"
if ((termination_grace < schema_shutdown_maximum / 1000 + 5 + 5 + 15)); then
  echo "termination grace does not cover maximum ordered shutdown with margin" >&2
  exit 1
fi

rollout_base="$temporary_directory/rollout-base"
cp -R "$repository_root/deploy/k8s/base/egress-gateway" "$rollout_base"
jq '.metadata.revision = "2026-08-07.2"' "$rollout_base/policy.json" >"$rollout_base/policy.next.json"
mv "$rollout_base/policy.next.json" "$rollout_base/policy.json"
next_digest="$(cd "$repository_root/services/external/egress-gateway" && go run ./cmd/policy-digest "$rollout_base/policy.json")"
sed -i \
  -e 's/2026-08-07\.1/2026-08-07.2/g' \
  -e "s/$policy_digest/$next_digest/g" \
  "$rollout_base/deployment.yaml"
next_render="$temporary_directory/next-policy.yaml"
kubectl kustomize "$rollout_base" >"$next_render"
current_policy_name="$(yq -r 'select(.kind == "ConfigMap" and .immutable == true and .data."policy.json" != "") | .metadata.name' "$base_render")"
next_policy_name="$(yq -r 'select(.kind == "ConfigMap" and .immutable == true and .data."policy.json" != "") | .metadata.name' "$next_render")"
next_reference="$(yq -r 'select(.kind == "Deployment" and .metadata.name == "egress-gateway") |
  .spec.template.spec.volumes[] | select(.name == "policy") | .configMap.name' "$next_render")"
if [[ "$current_policy_name" == "$next_policy_name" || "$next_policy_name" != "$next_reference" ]]; then
  echo "immutable policy rollout did not create and select a new content-addressed object" >&2
  exit 1
fi

echo "Egress gateway contract, environment renders, immutable rollout and network boundary verification passed."
