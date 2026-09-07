#!/usr/bin/env bash
set -euo pipefail
# Миграция только выбранной фазы disposable render, до её SSA.
fail() { printf 'Worker grant strategy migration failed: %s\n' "$1" >&2; exit 1; }
[[ $# == 1 && -f "$1" ]] || fail 'render is required'
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
temporary=$(mktemp -d)
trap 'rm -rf -- "$temporary"' EXIT
yq -o=json -I=0 '.' "$1" | jq -s 'map(select(.kind != null))' >"$temporary/resources.json"
# Существующий closed registry также отвергает неизвестный grant workload.
jq -f "$root/tools/dev/worker-grant-rollout.jq" "$temporary/resources.json" >"$temporary/checked.json"
jq -c '.[] | select(.kind == "Deployment" and
  any(.spec.template.spec.containers[]?, .spec.template.spec.initContainers[]?;
    ((.name | endswith("platform-worker-grant-agent"))) or
    any(((.command // []) + (.args // []))[];
      test("(^|/)internal-rpc-authority-platform-worker-grant-agent$"))))' "$temporary/resources.json" >"$temporary/workers.jsonl"
while IFS= read -r desired; do
  jq -e '.metadata.namespace == "kodex-system" and .spec.replicas == 1 and
    .spec.template.metadata.labels."kodex.dev/environment" == "staging" and
    .spec.template.metadata.labels."kodex.dev/local-profile" == "hot-reload" and
    (.spec.template.metadata.labels."kodex.dev/profile" | . == "web-only" or . == "web-with-mattermost") and
    .spec.strategy == {type:"Recreate"}' <<<"$desired" >/dev/null || fail 'desired strategy is invalid'
  name=$(jq -r '.metadata.name' <<<"$desired")
  kubectl -n kodex-system get deployment "$name" --ignore-not-found --request-timeout=20s -o json >"$temporary/current.json" 2>/dev/null || fail 'read failed'
  [[ -s "$temporary/current.json" ]] || continue
  jq -e --arg name "$name" --argjson desired "$desired" '
    .kind == "Deployment" and .metadata.name == $name and .metadata.namespace == "kodex-system" and
    (.metadata.uid | type == "string" and length > 0) and
    (.metadata.resourceVersion | type == "string" and length > 0) and
    .metadata.deletionTimestamp == null and .spec.selector == $desired.spec.selector and
    .spec.template.metadata.labels."kodex.dev/environment" == "staging" and
    .spec.template.metadata.labels."kodex.dev/local-profile" == "hot-reload" and
    (.spec.template.metadata.labels."kodex.dev/profile" | . == "web-only" or . == "web-with-mattermost") and
    any(.spec.template.spec.containers[]?, .spec.template.spec.initContainers[]?;
      (.name | endswith("platform-worker-grant-agent")) or
      any(((.command // []) + (.args // []))[];
        test("(^|/)internal-rpc-authority-platform-worker-grant-agent$")))
  ' "$temporary/current.json" >/dev/null || fail 'existing workload binding is invalid'
  if jq -e '.spec.strategy == {type:"Recreate"}' "$temporary/current.json" >/dev/null; then
    continue
  fi
  jq -e '.spec.strategy.type == "RollingUpdate"' "$temporary/current.json" >/dev/null || fail 'existing strategy is unsupported'
  jq '[
    {op:"test",path:"/metadata/uid",value:.metadata.uid},
    {op:"test",path:"/metadata/resourceVersion",value:.metadata.resourceVersion},
    {op:"test",path:"/spec/strategy",value:.spec.strategy},
    {op:"replace",path:"/spec/strategy",value:{type:"Recreate"}},
    {op:"add",path:"/spec/replicas",value:1}
  ]' "$temporary/current.json" >"$temporary/patch.json"
  kubectl -n kodex-system patch deployment "$name" --type=json --field-manager=kodex-local-dev \
    --request-timeout=20s --patch-file "$temporary/patch.json" >/dev/null 2>&1 || fail 'atomic patch failed'
  kubectl -n kodex-system get deployment "$name" --request-timeout=20s -o json >"$temporary/after.json" 2>/dev/null || fail 'readback failed'
  jq -e --slurpfile before "$temporary/current.json" '
    .metadata.uid == $before[0].metadata.uid and .metadata.deletionTimestamp == null and
    .spec.replicas == 1 and .spec.strategy == {type:"Recreate"} and
    .spec.template == $before[0].spec.template
  ' "$temporary/after.json" >/dev/null || fail 'readback mismatch'
done <"$temporary/workers.jsonl"
