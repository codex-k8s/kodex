#!/usr/bin/env bash
set -euo pipefail

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
for dependency in go docker kubectl yq jq timeout; do
  command -v "$dependency" >/dev/null || { printf '%s is required\n' "$dependency" >&2; exit 1; }
done

(
  cd "$repository_root/services/jobs/automation-scheduler"
  timeout 180s go test -race -count=1 -timeout=120s ./...
)
(
  cd "$repository_root/services/internal/control-plane"
  timeout 180s go test -race -count=1 -timeout=120s ./internal/domain/service/schedule ./internal/domain/service/platform ./internal/transport/grpc
)
KODEX_CONTROL_PLANE_TEST_FILTER='^TestBootstrapComponent$/.*schedule' \
  timeout 300s bash "$repository_root/scripts/tests/control-plane-postgres-test.sh"

for environment in staging production; do
  render="$temporary_directory/$environment.yaml"
  timeout 60s bash "$repository_root/tools/render-automation-scheduler.sh" "$environment" \
    "sha256:$(printf '%064d' 1)" "sha256:$(printf '%064d' 2)" registry.example.test >"$render"
  yq -o=json -I=0 '.' "$render" | jq -se '
    [ .[] | select(.kind == "Deployment" and .metadata.name == "automation-scheduler") ] as $deployments |
    ($deployments | length) == 1 and
    ($deployments[0].spec.replicas == 2) and
    ($deployments[0].spec.template.spec | (.containers + .initContainers) |
      map(select(.image | contains("/kodex/internal-rpc-authority@sha256:"))) | length) == 3 and
    all(.[] | select(.kind == "NetworkPolicy") | .spec.egress[]?;
      (.to | length) > 0 and all(.to[]; has("podSelector") or has("ipBlock"))) and
    all(.[] | select(.kind == "PrometheusRule") | .spec.groups[].rules[];
      .annotations.runbook_url | startswith("https://"))
  ' >/dev/null
done

if bash "$repository_root/tools/render-automation-scheduler.sh" staging \
  "sha256:$(printf '%064d' 0)" "sha256:$(printf '%064d' 2)" registry.example.test >/dev/null 2>&1; then
  printf 'Zero image digest was accepted\n' >&2
  exit 1
fi
printf 'Automation scheduler targeted tests passed\n'
