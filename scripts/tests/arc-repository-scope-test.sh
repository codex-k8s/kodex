#!/usr/bin/env bash
set -euo pipefail
umask 077

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
bootstrap="$repository_root/infra/arc/bootstrap.sh"
policy_bootstrap="$repository_root/infra/github/bootstrap-actions-policy.sh"
materializer="$repository_root/infra/github/materialize-actions-policy-inputs.sh"
owner_gate="$repository_root/infra/arc/job-started-owner-gate.sh"
network_policy_renderer="$repository_root/infra/arc/render-network-policy.sh"
helm_values_renderer="$repository_root/infra/arc/render-helm-values.sh"
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT

bash -n "$bootstrap" "$policy_bootstrap" "$materializer" "$owner_gate" \
  "$network_policy_renderer" "$helm_values_renderer"

printf '%040d\n' 0 >"$temporary_directory/workflow-sha"
printf '1\n' >"$temporary_directory/build-owner"
printf '1\n' >"$temporary_directory/deploy-owner"
printf 'fixture-pat\n' >"$temporary_directory/pat"
printf '1\n' >"$temporary_directory/app-id"
printf '1\n' >"$temporary_directory/app-installation-id"
printf '%s\n' '-----BEGIN PRIVATE KEY-----' 'fixture' '-----END PRIVATE KEY-----' \
  >"$temporary_directory/app-private-key"
chmod 0600 "$temporary_directory"/*

common_arguments=(
  --context impossible-context
  --mode preflight
  --registry-namespace fixture-registry
  --release-registry-host registry.example.test
  --workflow-sha-file "$temporary_directory/workflow-sha"
  --build-owner-actor-id-file "$temporary_directory/build-owner"
  --deploy-owner-actor-id-file "$temporary_directory/deploy-owner"
)
if "$bootstrap" "${common_arguments[@]}" \
  --github-pat-file "$temporary_directory/pat" \
  --github-app-id-file "$temporary_directory/app-id" \
  --github-app-installation-id-file "$temporary_directory/app-installation-id" \
  --github-app-private-key-file "$temporary_directory/app-private-key" >/dev/null 2>&1; then
  printf 'ARC bootstrap accepted simultaneous PAT and GitHub App modes\n' >&2
  exit 1
fi
if "$bootstrap" "${common_arguments[@]}" \
  --github-app-id-file "$temporary_directory/app-id" >/dev/null 2>&1; then
  printf 'ARC bootstrap accepted an incomplete GitHub App mode\n' >&2
  exit 1
fi
if "$bootstrap" "${common_arguments[@]}" --github-pat-file "$temporary_directory/pat" \
  >"$temporary_directory/pat-only.out" 2>"$temporary_directory/pat-only.err"; then
  printf 'ARC bootstrap unexpectedly accepted an impossible Kubernetes context\n' >&2
  exit 1
fi
grep -Fq 'Kubernetes context mismatch' "$temporary_directory/pat-only.err" || {
  printf 'PAT-only mode did not reach the Kubernetes context gate\n' >&2
  exit 1
}
if "$bootstrap" "${common_arguments[@]}" \
  --release-registry-host https://registry.example.test \
  --github-pat-file "$temporary_directory/pat" >/dev/null 2>&1; then
  printf 'ARC bootstrap accepted a registry URL instead of an exact DNS host\n' >&2
  exit 1
fi

"$network_policy_renderer" 10.20.30.40/32 10.20.30.41/32 fixture-registry \
  registry.example.test \
  "$temporary_directory/network-policy.yaml"
rg -q '__REGISTRY_|matter-kodex-prod' "$temporary_directory/network-policy.yaml" && {
  printf 'ARC network policy retained an unresolved or legacy registry locator\n' >&2
  exit 1
}
mkdir -p "$temporary_directory/helm-values"
"$helm_values_renderer" 10.96.0.1 \
  0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef \
  kodex-registry.fixture-registry.svc.cluster.local \
  "$temporary_directory/helm-values" >/dev/null
rg -q '__REGISTRY_HOST__|matter-kodex-prod' "$temporary_directory/helm-values" && {
  printf 'ARC Helm values retained an unresolved or legacy registry locator\n' >&2
  exit 1
}

yq -e '
  .githubConfigUrl == "https://github.com/codex-k8s/kodex" and
  .githubConfigSecret == "kodex-github-runner-auth" and
  (.runnerGroup == null) and
  .runnerScaleSetName == "kodex-build" and
  ([.template.spec.containers[] | select(.name == "runner") | .env[] |
    select(.name == "ACTIONS_RUNNER_HOOK_JOB_STARTED" and
      .value == "/var/run/kodex-owner-gate/job-started.sh")] | length) == 1 and
  ([.template.spec.volumes[] | select(.name == "owner-gate" and
    .configMap.name == "kodex-runner-owner-gate")] | length) == 1 and
  ([.template.spec.volumes[] | select(.name == "tools" and
    .emptyDir.sizeLimit == "256Mi")] | length) == 1 and
  (.template.spec.hostUsers == null) and
  ([.template.spec.containers[] | select(.name == "buildkitd" and
    (.args | any_c(. == "--oci-worker-no-process-sandbox")) and
    .securityContext.runAsNonRoot == true and
    .securityContext.runAsUser == 1000 and
    .securityContext.allowPrivilegeEscalation == true and
    .securityContext.appArmorProfile.type == "Unconfined" and
    .securityContext.seccompProfile.type == "Unconfined" and
    (.securityContext.capabilities.drop | length) == 1 and
    .securityContext.capabilities.drop[0] == "ALL" and
    (.securityContext.capabilities.add | length) == 2 and
    (.securityContext.capabilities.add | any_c(. == "SETUID")) and
    (.securityContext.capabilities.add | any_c(. == "SETGID")))] | length) == 1
' "$repository_root/infra/arc/build-runner-values.yaml" >/dev/null
yq -e '
  .githubConfigUrl == "https://github.com/codex-k8s/kodex" and
  .githubConfigSecret == "kodex-github-runner-auth" and
  (.runnerGroup == null) and
  .runnerScaleSetName == "kodex-deploy" and
  ([.template.spec.containers[] | select(.name == "runner") | .env[] |
    select(.name == "ACTIONS_RUNNER_HOOK_JOB_STARTED" and
      .value == "/var/run/kodex-owner-gate/job-started.sh")] | length) == 1 and
  ([.template.spec.volumes[] | select(.name == "owner-gate" and
    .configMap.name == "kodex-runner-owner-gate")] | length) == 1 and
  ([.template.spec.volumes[] | select(.name == "tools" and
    .emptyDir.sizeLimit == "256Mi")] | length) == 1
' "$repository_root/infra/arc/deploy-runner-values.yaml" >/dev/null
yq -e '
  .jobs.build."runs-on" == "kodex-build"
' "$repository_root/.github/workflows/build-release.yml" >/dev/null
yq -e '
  .jobs.render."runs-on" == "kodex-deploy"
' "$repository_root/.github/workflows/deploy-production.yml" >/dev/null

grep -Fq '!has(object.spec.runnerGroup)' "$repository_root/infra/arc/namespace-rbac.yaml"
grep -Fq -- '--from-file=github_token="$github_pat_file"' "$bootstrap"
grep -Fq "'[\"github_token\"]'" "$bootstrap"
for gate_variable in GITHUB_REPOSITORY GITHUB_EVENT_NAME GITHUB_REF GITHUB_WORKFLOW_REF \
  GITHUB_WORKFLOW_SHA GITHUB_SHA GITHUB_ACTOR_ID GITHUB_JOB; do
  grep -Fq "$gate_variable" "$owner_gate"
done
if rg -q 'runnerGroup:|runner-groups|kodex-production-(build|deploy)([^a-z]|$)' \
  "$repository_root/infra/arc/build-runner-values.yaml" \
  "$repository_root/infra/arc/deploy-runner-values.yaml" \
  "$policy_bootstrap"; then
  printf 'Repo-scoped ARC path still contains an organization runner group\n' >&2
  exit 1
fi
if rg -q 'reviewers|wait_timer' "$policy_bootstrap"; then
  printf 'GitHub Environment payload still requests a plan-gated protection rule\n' >&2
  exit 1
fi
if rg -q '\.items \| length == 1 and all\(\.items\[\]' "$bootstrap"; then
  printf 'ARC readback reuses an object path after switching jq context to its items array\n' >&2
  exit 1
fi
if rg -U -q 'auth can-i[^\n]*(\n[^\n]*){0,2}\| grep -qx no' "$bootstrap"; then
  printf 'Expected Kubernetes authorization denial still relies on a pipefail pipeline\n' >&2
  exit 1
fi
grep -Fq '(.spec.replicas // 0) == 0' "$bootstrap"
grep -Fq '$items[0].spec.ephemeralRunnerSetName' "$bootstrap"
grep -Fq 'render_owner_gate_config "$build_namespace" .github/workflows/build-release.yml build' \
  "$bootstrap"
grep -Fq 'render_owner_gate_config "$deploy_namespace" .github/workflows/deploy-production.yml render' \
  "$bootstrap"
if grep -Fq 'render_owner_gate_config "$deploy_namespace" .github/workflows/deploy-production.yml deploy' \
  "$bootstrap"; then
  printf 'Deploy runner owner gate still expects a non-existent deploy job\n' >&2
  exit 1
fi
yq -r 'select(.kind == "ConfigMap" and .metadata.name == "kodex-ci-egress-proxy") |
  .data."envoy.yaml"' "$temporary_directory/network-policy.yaml" \
  >"$temporary_directory/envoy.yaml"
yq -o=json '.' "$temporary_directory/envoy.yaml" | jq -e '
  def authority_allowed($routes; $authority):
    any($routes[];
      .match.headers[0] as $header |
      ($header.name == ":authority") and
      (($header.string_match.exact? == $authority) or
        (($header.string_match.safe_regex.regex? // "") as $regex |
          ($regex != "" and ($authority | test($regex))))));
  .static_resources.listeners[0].filter_chains[0].filters[0].typed_config as $hcm |
  $hcm.route_config.virtual_hosts[0] as $virtual_host |
  $virtual_host.routes as $routes |
  $hcm.upgrade_configs[0] as $hcm_upgrade |
  ($virtual_host.domains == ["*"]) and
  ($routes | length == 24) and
  (all($routes[];
    .match.connect_matcher == {} and
    (.match.headers | length == 1) and .match.headers[0].name == ":authority" and
    ((.match.headers[0].string_match.safe_regex.regex? // "") | length < 80) and
    .route.cluster == "dynamic_forward_proxy" and
    (.route.upgrade_configs | length == 1) and
    .route.upgrade_configs[0].upgrade_type == "CONNECT" and
    .route.upgrade_configs[0].connect_config == {})) and
  (all("github.com:443", "broker.actions.githubusercontent.com:443",
    "raw.githubusercontent.com:443", "avatars.githubassets.com:443",
    "ghcr.io:443", "registry.example.test:443", "gcr.io:443", "registry-1.docker.io:443",
    "production.cloudfront.docker.com:443",
    "proxy.golang.org:443", "sum.golang.org:443", "storage.googleapis.com:443",
    "registry.npmjs.org:443", "dl-cdn.alpinelinux.org:443",
    "deb.debian.org:443", "security.debian.org:443", "get.helm.sh:443",
    "cdn.playwright.dev:443", "playwright.download.prss.microsoft.com:443",
    "githubactionsresults.blob.core.windows.net:443";
      authority_allowed($routes; .))) and
  (all("example.com:443", "github.com:80", "gcr.io.attacker.invalid:443",
    "github.com.attacker.invalid:443",
    "broker.actions.githubusercontent.com.attacker.invalid:443",
    "production.cloudflare.docker.com:443",
    "proxy.golang.org.attacker.invalid:443", "proxy.golang.org:80",
    "registry.npmjs.org.attacker.invalid:443", "cdn.playwright.dev:80",
    "blob.core.windows.net:443",
    "githubactionsresults.blob.core.windows.net.attacker.invalid:443";
      (authority_allowed($routes; .) | not))) and
  ($hcm_upgrade.upgrade_type == "CONNECT") and
  ($hcm_upgrade | has("connect_config") | not) and
  ([.. | objects | select(has("connect_config"))] | length == ($routes | length))
' >/dev/null || {
  printf 'Envoy CONNECT termination is not configured on the exact route\n' >&2
  exit 1
}

"$network_policy_renderer" 10.43.198.224/32 192.0.2.10/32 \
  fixture-registry registry.example.test "$temporary_directory/rendered-network-policy.yaml"
yq -o=json eval-all '.' "$temporary_directory/rendered-network-policy.yaml" | jq -sc -e '
  map(select(.kind == "NetworkPolicy" and .metadata.namespace == "kodex-ci" and
    .metadata.name == "build-registry")) |
  length == 1 and
  .[0].spec.egress[0].to[0].namespaceSelector.matchLabels."kubernetes.io/metadata.name" ==
    "fixture-registry" and
  ([.[0].spec.egress[0].to[] | select(.ipBlock != null) | .ipBlock.cidr] | sort) ==
    ["10.43.198.224/32","192.0.2.10/32"] and
  ([.[0].spec.egress[0].ports[].port] | sort) == [5000,5001]
' >/dev/null || {
  printf 'Rendered build registry hostNetwork destination is not exact\n' >&2
  exit 1
}
if "$network_policy_renderer" 0.0.0.0/0 192.0.2.10/32 \
  fixture-registry registry.example.test \
  "$temporary_directory/forbidden-registry-network-policy.yaml" >/dev/null 2>&1; then
  printf 'Registry network policy renderer accepted a non-/32 destination\n' >&2
  exit 1
fi
if "$network_policy_renderer" 999.0.0.1/32 192.0.2.10/32 \
  fixture-registry registry.example.test \
  "$temporary_directory/forbidden-registry-network-policy.yaml" >/dev/null 2>&1; then
  printf 'Registry network policy renderer accepted an invalid IPv4 destination\n' >&2
  exit 1
fi
if "$network_policy_renderer" 10.43.198.224/32 192.0.2.10/32 \
  fixture-registry https://registry.example.test \
  "$temporary_directory/forbidden-registry-network-policy.yaml" >/dev/null 2>&1; then
  printf 'Registry network policy renderer accepted a registry URL instead of an exact DNS host\n' >&2
  exit 1
fi
if "$network_policy_renderer" 10.43.198.224/32 192.0.2.10/32 \
  fixture-registry registry.example.test:443 \
  "$temporary_directory/forbidden-registry-network-policy.yaml" >/dev/null 2>&1; then
  printf 'Registry network policy renderer accepted a registry host with a port\n' >&2
  exit 1
fi
for proxy_namespace in kodex-ci kodex-ci-deploy; do
  rendered_config="$temporary_directory/envoy-$proxy_namespace.yaml"
  PROXY_NAMESPACE=$proxy_namespace yq -r '
    select(.kind == "ConfigMap" and .metadata.namespace == strenv(PROXY_NAMESPACE) and
      .metadata.name == "kodex-ci-egress-proxy") | .data."envoy.yaml"
  ' "$temporary_directory/rendered-network-policy.yaml" >"$rendered_config"
  expected_checksum=$(sha256sum "$rendered_config" | awk '{print $1}')
  PROXY_NAMESPACE=$proxy_namespace CONFIG_CHECKSUM=$expected_checksum yq -e '
    select(.kind == "Deployment" and .metadata.namespace == strenv(PROXY_NAMESPACE) and
      .metadata.name == "kodex-ci-egress-proxy") |
    .spec.template.metadata.annotations."kodex.dev/envoy-config-sha256" ==
      strenv(CONFIG_CHECKSUM)
  ' "$temporary_directory/rendered-network-policy.yaml" >/dev/null || {
    printf 'Rendered Envoy checksum does not trigger an exact proxy rollout\n' >&2
    exit 1
  }
done

mkdir -p -- "$temporary_directory/helm-values"
fixture_proxy_sha=$(printf '%064d' 0)
"$helm_values_renderer" 10.43.0.1 "$fixture_proxy_sha" \
  kodex-registry.fixture-registry.svc.cluster.local \
  "$temporary_directory/helm-values" >/dev/null
expected_kubernetes_no_proxy='10.43.0.1,kubernetes.default.svc,kubernetes.default.svc.cluster.local,.svc,.svc.cluster.local,localhost,127.0.0.1'
for controller_values in controller-values controller-deploy-values; do
  NO_PROXY_EXPECTED=$expected_kubernetes_no_proxy \
    PROXY_SHA_EXPECTED=$fixture_proxy_sha yq -e '
    (.podAnnotations."kodex.dev/egress-proxy-config-sha256" ==
      strenv(PROXY_SHA_EXPECTED)) and
    ([.env[] | select(.name == "NO_PROXY" and .value == strenv(NO_PROXY_EXPECTED))] |
      length == 1)
  ' "$temporary_directory/helm-values/$controller_values.yaml" >/dev/null
done
NO_PROXY_EXPECTED=$expected_kubernetes_no_proxy yq -e '
  ([.listenerTemplate.spec.containers[].env[] |
    select(.name == "NO_PROXY" and .value == strenv(NO_PROXY_EXPECTED))] | length == 1) and
  ([.listenerTemplate.spec.containers[] | select(.name == "listener" and
    .securityContext.runAsNonRoot == true and
    .securityContext.allowPrivilegeEscalation == false and
    (.securityContext.capabilities.drop | length) == 1 and
    .securityContext.capabilities.drop[0] == "ALL" and
    .securityContext.seccompProfile.type == "RuntimeDefault")] | length == 1)
' "$temporary_directory/helm-values/build-runner-values.yaml" >/dev/null
REGISTRY_NO_PROXY='kodex-registry.fixture-registry.svc.cluster.local,localhost,127.0.0.1' \
  yq -e '
    ([.template.spec.containers[] | select(.name == "runner" or .name == "buildkitd") |
      .env[] | select(.name == "NO_PROXY" and .value == strenv(REGISTRY_NO_PROXY))] |
      length == 2)
  ' "$temporary_directory/helm-values/build-runner-values.yaml" >/dev/null
NO_PROXY_EXPECTED=$expected_kubernetes_no_proxy yq -e '
  ([.listenerTemplate.spec.containers[].env[] |
    select(.name == "NO_PROXY" and .value == strenv(NO_PROXY_EXPECTED))] | length == 1) and
  ([.listenerTemplate.spec.containers[] | select(.name == "listener" and
    .securityContext.runAsNonRoot == true and
    .securityContext.allowPrivilegeEscalation == false and
    (.securityContext.capabilities.drop | length) == 1 and
    .securityContext.capabilities.drop[0] == "ALL" and
    .securityContext.seccompProfile.type == "RuntimeDefault")] | length == 1) and
  ([.template.spec.containers[] | select(.name == "runner") | .env[] |
    select(.name == "NO_PROXY" and .value == strenv(NO_PROXY_EXPECTED))] | length == 1)
' "$temporary_directory/helm-values/deploy-runner-values.yaml" >/dev/null
if rg -q '__KUBERNETES_API_SERVICE_IP__|__EGRESS_PROXY_CONFIG_SHA256__|__REGISTRY_HOST__' \
  "$temporary_directory/helm-values"; then
  printf 'Rendered ARC Helm values contain an unresolved placeholder\n' >&2
  exit 1
fi
if "$helm_values_renderer" 10.43.0.999 "$fixture_proxy_sha" \
  kodex-registry.fixture-registry.svc.cluster.local \
  "$temporary_directory/helm-values" \
  >/dev/null 2>&1; then
  printf 'ARC Helm values renderer accepted an invalid Kubernetes API IP\n' >&2
  exit 1
fi
if "$helm_values_renderer" 10.43.0.1 invalid-sha \
  kodex-registry.fixture-registry.svc.cluster.local \
  "$temporary_directory/helm-values" >/dev/null 2>&1; then
  printf 'ARC Helm values renderer accepted an invalid proxy config SHA-256\n' >&2
  exit 1
fi

printf 'ARC repository-scope negative checks completed\n'
