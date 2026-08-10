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

yq -e '
  .githubConfigUrl == "https://github.com/codex-k8s/matter-codex" and
  .githubConfigSecret == "mattercodex-github-runner-auth" and
  (.runnerGroup == null) and
  .runnerScaleSetName == "mattercodex-build" and
  ([.template.spec.containers[] | select(.name == "runner") | .env[] |
    select(.name == "ACTIONS_RUNNER_HOOK_JOB_STARTED" and
      .value == "/var/run/mattercodex-owner-gate/job-started.sh")] | length) == 1 and
  ([.template.spec.volumes[] | select(.name == "owner-gate" and
    .configMap.name == "mattercodex-runner-owner-gate")] | length) == 1
' "$repository_root/infra/arc/build-runner-values.yaml" >/dev/null
yq -e '
  .githubConfigUrl == "https://github.com/codex-k8s/matter-codex" and
  .githubConfigSecret == "mattercodex-github-runner-auth" and
  (.runnerGroup == null) and
  .runnerScaleSetName == "mattercodex-deploy" and
  ([.template.spec.containers[] | select(.name == "runner") | .env[] |
    select(.name == "ACTIONS_RUNNER_HOOK_JOB_STARTED" and
      .value == "/var/run/mattercodex-owner-gate/job-started.sh")] | length) == 1 and
  ([.template.spec.volumes[] | select(.name == "owner-gate" and
    .configMap.name == "mattercodex-runner-owner-gate")] | length) == 1
' "$repository_root/infra/arc/deploy-runner-values.yaml" >/dev/null
yq -e '
  .jobs.build."runs-on" == "mattercodex-build"
' "$repository_root/.github/workflows/build-release.yml" >/dev/null
yq -e '
  .jobs.deploy."runs-on" == "mattercodex-deploy"
' "$repository_root/.github/workflows/deploy-production.yml" >/dev/null

grep -Fq '!has(object.spec.runnerGroup)' "$repository_root/infra/arc/namespace-rbac.yaml"
grep -Fq -- '--from-file=github_token="$github_pat_file"' "$bootstrap"
grep -Fq "'[\"github_token\"]'" "$bootstrap"
for gate_variable in GITHUB_REPOSITORY GITHUB_EVENT_NAME GITHUB_REF GITHUB_WORKFLOW_REF \
  GITHUB_WORKFLOW_SHA GITHUB_SHA GITHUB_ACTOR_ID GITHUB_JOB; do
  grep -Fq "$gate_variable" "$owner_gate"
done
if rg -q 'runnerGroup:|runner-groups|mattercodex-production-(build|deploy)([^a-z]|$)' \
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
yq -r 'select(.kind == "ConfigMap" and .metadata.name == "mattercodex-ci-egress-proxy") |
  .data."envoy.yaml"' "$repository_root/infra/arc/network-policy.yaml" \
  >"$temporary_directory/envoy.yaml"
yq -o=json '.' "$temporary_directory/envoy.yaml" | jq -e '
  .static_resources.listeners[0].filter_chains[0].filters[0].typed_config as $hcm |
  $hcm.route_config.virtual_hosts[0] as $virtual_host |
  $virtual_host.routes[0] as $connect_route |
  $connect_route.route.upgrade_configs[0] as $route_upgrade |
  $hcm.upgrade_configs[0] as $hcm_upgrade |
  $connect_route.match.headers[0] as $authority_match |
  $authority_match.string_match.safe_regex.regex as $authority_regex |
  ($virtual_host.domains == ["*"]) and
  ($authority_match.name == ":authority") and
  (all("github.com:443", "broker.actions.githubusercontent.com:443",
    "raw.githubusercontent.com:443", "avatars.githubassets.com:443",
    "ghcr.io:443", "registry-1.docker.io:443"; test($authority_regex))) and
  (all("example.com:443", "github.com:80", "github.com.attacker.invalid:443",
    "broker.actions.githubusercontent.com.attacker.invalid:443";
      (test($authority_regex) | not))) and
  ($hcm_upgrade.upgrade_type == "CONNECT") and
  ($hcm_upgrade | has("connect_config") | not) and
  ($route_upgrade.upgrade_type == "CONNECT") and
  ($route_upgrade.connect_config == {}) and
  ([.. | objects | select(has("connect_config"))] | length == 1)
' >/dev/null || {
  printf 'Envoy CONNECT termination is not configured on the exact route\n' >&2
  exit 1
}

"$network_policy_renderer" "$temporary_directory/rendered-network-policy.yaml"
for proxy_namespace in mattercodex-ci mattercodex-ci-deploy; do
  rendered_config="$temporary_directory/envoy-$proxy_namespace.yaml"
  PROXY_NAMESPACE=$proxy_namespace yq -r '
    select(.kind == "ConfigMap" and .metadata.namespace == strenv(PROXY_NAMESPACE) and
      .metadata.name == "mattercodex-ci-egress-proxy") | .data."envoy.yaml"
  ' "$temporary_directory/rendered-network-policy.yaml" >"$rendered_config"
  expected_checksum=$(sha256sum "$rendered_config" | awk '{print $1}')
  PROXY_NAMESPACE=$proxy_namespace CONFIG_CHECKSUM=$expected_checksum yq -e '
    select(.kind == "Deployment" and .metadata.namespace == strenv(PROXY_NAMESPACE) and
      .metadata.name == "mattercodex-ci-egress-proxy") |
    .spec.template.metadata.annotations."mattercodex.dev/envoy-config-sha256" ==
      strenv(CONFIG_CHECKSUM)
  ' "$temporary_directory/rendered-network-policy.yaml" >/dev/null || {
    printf 'Rendered Envoy checksum does not trigger an exact proxy rollout\n' >&2
    exit 1
  }
done

mkdir -p -- "$temporary_directory/helm-values"
"$helm_values_renderer" 10.43.0.1 "$temporary_directory/helm-values" >/dev/null
expected_kubernetes_no_proxy='10.43.0.1,kubernetes.default.svc,kubernetes.default.svc.cluster.local,.svc,.svc.cluster.local,localhost,127.0.0.1'
for controller_values in controller-values controller-deploy-values; do
  NO_PROXY_EXPECTED=$expected_kubernetes_no_proxy yq -e '
    [.env[] | select(.name == "NO_PROXY" and .value == strenv(NO_PROXY_EXPECTED))] |
      length == 1
  ' "$temporary_directory/helm-values/$controller_values.yaml" >/dev/null
done
NO_PROXY_EXPECTED=$expected_kubernetes_no_proxy yq -e '
  [.listenerTemplate.spec.containers[].env[] |
    select(.name == "NO_PROXY" and .value == strenv(NO_PROXY_EXPECTED))] | length == 1
' "$temporary_directory/helm-values/build-runner-values.yaml" >/dev/null
NO_PROXY_EXPECTED=$expected_kubernetes_no_proxy yq -e '
  ([.listenerTemplate.spec.containers[].env[] |
    select(.name == "NO_PROXY" and .value == strenv(NO_PROXY_EXPECTED))] | length == 1) and
  ([.template.spec.containers[] | select(.name == "runner") | .env[] |
    select(.name == "NO_PROXY" and .value == strenv(NO_PROXY_EXPECTED))] | length == 1)
' "$temporary_directory/helm-values/deploy-runner-values.yaml" >/dev/null
if rg -q '__KUBERNETES_API_SERVICE_IP__' "$temporary_directory/helm-values"; then
  printf 'Rendered ARC Helm values contain an unresolved Kubernetes API IP\n' >&2
  exit 1
fi
if "$helm_values_renderer" 10.43.0.999 "$temporary_directory/helm-values" \
  >/dev/null 2>&1; then
  printf 'ARC Helm values renderer accepted an invalid Kubernetes API IP\n' >&2
  exit 1
fi

printf 'ARC repository-scope negative checks completed\n'
