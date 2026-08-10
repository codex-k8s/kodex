#!/usr/bin/env bash
set -euo pipefail
umask 077

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
bootstrap="$repository_root/infra/arc/bootstrap.sh"
policy_bootstrap="$repository_root/infra/github/bootstrap-actions-policy.sh"
materializer="$repository_root/infra/github/materialize-actions-policy-inputs.sh"
owner_gate="$repository_root/infra/arc/job-started-owner-gate.sh"
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT

bash -n "$bootstrap" "$policy_bootstrap" "$materializer" "$owner_gate"

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
  .static_resources.listeners[0].filter_chains[0].filters[0].typed_config
    .route_config.virtual_hosts[0].routes[0].route.upgrade_configs[0] as $upgrade |
  ($upgrade.upgrade_type == "CONNECT") and
  ($upgrade.connect_config == {}) and
  ([.. | objects | select(has("connect_config"))] | length == 1)
' >/dev/null || {
  printf 'Envoy CONNECT termination is not configured on the exact route\n' >&2
  exit 1
}

printf 'ARC repository-scope negative checks completed\n'
