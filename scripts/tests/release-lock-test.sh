#!/usr/bin/env bash
set -euo pipefail

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
validator="$repository_root/tools/release/validate-release-lock.sh"
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
source_sha=1111111111111111111111111111111111111111
digest=sha256:2222222222222222222222222222222222222222222222222222222222222222
jq --arg source_sha "$source_sha" --arg digest "$digest" '
  {schema_version:1,profile:"direct-production single-node prototype",source_sha:$source_sha,build_run_id:"123",registry_push:"matter-codex-registry.matter-kodex-prod.svc.cluster.local:5000",node_pull:"localhost:5001",images:[.images[]|{component,repository:("mattercodex/"+.component),digest:$digest,pull_ref:("localhost:5001/mattercodex/"+.component+"@"+$digest)}]}
' "$repository_root/tools/release/images.json" | jq -S . >"$temporary_directory/valid.json"
lock_sha=$(sha256sum "$temporary_directory/valid.json" | awk '{print $1}')
"$validator" --lock "$temporary_directory/valid.json" --source-sha "$source_sha" --sha256 "$lock_sha" >/dev/null

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
