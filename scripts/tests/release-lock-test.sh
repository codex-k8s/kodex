#!/usr/bin/env bash
set -euo pipefail

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
validator="$repository_root/tools/release/validate-release-lock.sh"
renderer="$repository_root/tools/release/render-direct-production.sh"
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
source_sha=1111111111111111111111111111111111111111
digest=sha256:2222222222222222222222222222222222222222222222222222222222222222
jq --arg source_sha "$source_sha" --arg digest "$digest" '
  {schema_version:1,profile:"direct-production single-node prototype",source_sha:$source_sha,build_run_id:"123",registry_push:"matter-codex-registry.matter-kodex-prod.svc.cluster.local:5000",node_pull:"localhost:5001",images:[.images[]|{component,repository:("mattercodex/"+.component),digest:$digest,pull_ref:("localhost:5001/mattercodex/"+.component+"@"+$digest)}]}
' "$repository_root/tools/release/images.json" | jq -S . >"$temporary_directory/valid.json"
lock_sha=$(sha256sum "$temporary_directory/valid.json" | awk '{print $1}')
"$validator" --lock "$temporary_directory/valid.json" --source-sha "$source_sha" --sha256 "$lock_sha" >/dev/null
"$renderer" --lock "$temporary_directory/valid.json" --source-sha "$source_sha" \
  --sha256 "$lock_sha" --output "$temporary_directory/direct-production.yaml" >/dev/null
for expected_resource in \
  Deployment/control-plane Deployment/runtime-controller Deployment/interaction-gateway \
  Deployment/integration-gateway Deployment/control-api-gateway Deployment/automation-scheduler \
  Job/control-plane-migrate Job/internal-rpc-authority-migrate StatefulSet/mattercodex-postgresql; do
  expected_kind=${expected_resource%%/*}
  expected_name=${expected_resource#*/}
  EXPECTED_KIND="$expected_kind" EXPECTED_NAME="$expected_name" yq eval-all -e '
    select(.kind == strenv(EXPECTED_KIND) and .metadata.name == strenv(EXPECTED_NAME)) |
    .metadata.namespace == "mattercodex-system" and
    .metadata.labels."mattercodex.dev/release-managed" == "true"
  ' "$temporary_directory/direct-production.yaml" >/dev/null || {
    printf 'Expected direct-production resource is absent: %s\n' "$expected_resource" >&2
    exit 1
  }
done
if grep -Eq '^kind: Ingress$|namespace: matter-kodex-prod$|sha256:0{64}' "$temporary_directory/direct-production.yaml"; then
  printf 'Direct-production render contains a forbidden marker\n' >&2
  exit 1
fi
while IFS= read -r image; do
  [[ "$image" != localhost:5001/mattercodex/* ]] ||
    grep -Fqx "$image" <(jq -r '.images[].pull_ref' "$temporary_directory/valid.json") || {
      printf 'Direct-production render contains an image outside the release lock\n' >&2
      exit 1
    }
done < <(yq eval-all -r '.. | .image?' "$temporary_directory/direct-production.yaml" | sed '/^---$/d;/^null$/d')
if yq eval-all -e 'select(.kind == "Deployment" and .metadata.name == "role-image-builder")' \
  "$temporary_directory/direct-production.yaml" >/dev/null 2>&1; then
  printf 'Deferred hardened supply-chain workload leaked into dark render\n' >&2
  exit 1
fi

jq '.images[0].pull_ref = "localhost:5001/mattercodex/control-plane:latest"' "$temporary_directory/valid.json" >"$temporary_directory/mutable.json"
mutable_sha=$(sha256sum "$temporary_directory/mutable.json" | awk '{print $1}')
if "$validator" --lock "$temporary_directory/mutable.json" --source-sha "$source_sha" --sha256 "$mutable_sha" >/dev/null 2>&1; then
  printf 'Mutable image reference was accepted\n' >&2
  exit 1
fi
if "$validator" --lock "$temporary_directory/valid.json" --source-sha 3333333333333333333333333333333333333333 --sha256 "$lock_sha" >/dev/null 2>&1; then
  printf 'Mismatched source SHA was accepted\n' >&2
  exit 1
fi
printf 'Release lock negative checks completed\n'
