#!/usr/bin/env bash
set -euo pipefail

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
image=${KODEX_WORKSPACE_TEST_IMAGE:?KODEX_WORKSPACE_TEST_IMAGE is required}
[[ "$image" =~ ^sha256:[a-f0-9]{64}$ ]] || exit 1
docker image inspect "$image" >/dev/null
fixture=$(mktemp -d)
container="kodex-workspace-parent-$$"
cleanup() {
  docker rm -f "$container" >/dev/null 2>&1 || true
  docker run --rm --pull=never --network none --user 0:0 --entrypoint /bin/sh \
    --mount "type=bind,src=$fixture,dst=/fixture" "$image" -ec 'rm -rf /fixture/*' >/dev/null 2>&1 || true
  rmdir "$fixture"
}
trap cleanup EXIT
chmod 0755 "$fixture"
mkdir "$fixture/workspace" "$fixture/state" "$fixture/input" "$fixture/knowledge"
(
  cd "$repository_root/services/jobs/agent-runner"
  CGO_ENABLED=0 GOWORK=off timeout 120s go build -trimpath -buildvcs=false -o "$fixture/runner" ./cmd/agent-runner
  CGO_ENABLED=0 GOWORK=off timeout 120s go test -c -o "$fixture/app.test" ./internal/app
)
root_fixture() {
  timeout 20s docker run --rm --pull=never --network none --user 0:0 --entrypoint /bin/sh \
    --mount "type=bind,src=$fixture,dst=/fixture" "$image" -ec "$1"
}
root_fixture 'chown 0:0 /fixture/runner /fixture/app.test; chmod 0555 /fixture/runner /fixture/app.test; chown 0:29000 /fixture/workspace /fixture/state /fixture/input /fixture/knowledge; chmod 2777 /fixture/workspace /fixture/state /fixture/input /fixture/knowledge'
common=(--rm --name "$container" --pull=never --network none --read-only --user 10001:10001 --group-add 29000 --cap-drop ALL --security-opt no-new-privileges
  --mount "type=bind,src=$fixture/workspace,dst=/workspace"
  --mount "type=bind,src=$fixture/runner,dst=/usr/local/bin/kodex-agent-runner,readonly")
prepare() {
  timeout 20s docker run "${common[@]}" --entrypoint /usr/local/bin/kodex-agent-runner "$image" runtime-prepare-workspace
}
materialize() {
  timeout 20s docker run "${common[@]}" \
    --mount "type=bind,src=$fixture/state,dst=/workspace/.kodex/state" \
    --mount "type=bind,src=$fixture/input,dst=/workspace/input" \
    --mount "type=bind,src=$fixture/knowledge,dst=/workspace/knowledge" \
    --mount "type=bind,src=$fixture/app.test,dst=/app.test,readonly" \
    -e KODEX_WORKSPACE_CONTAINER_TEST=1 --entrypoint /app.test "$image" \
    -test.run '^TestWorkspaceNestedMountContainer$' -test.timeout 10s
}
# Старый порядок: runtime создаёт root-owned промежуточный parent nested mount.
status=0; materialize >"$fixture/old.log" 2>&1 || status=$?
[[ "$status" == 1 ]] && grep -q 'workspace directory component is unsafe' "$fixture/old.log"
root_fixture 'rm -rf /fixture/workspace/.kodex'
prepare
prepare
materialize
materialize
root_fixture 'test "$(stat -c %u:%g:%a /fixture/workspace/.kodex)" = 10001:29000:2770; rm -rf /fixture/workspace/.kodex; ln -s /tmp /fixture/workspace/.kodex'
status=0; prepare >"$fixture/symlink.log" 2>&1 || status=$?
[[ "$status" == 1 ]]
root_fixture 'rm /fixture/workspace/.kodex; mkdir /fixture/workspace/.kodex; chown 10002:29000 /fixture/workspace/.kodex; chmod 2770 /fixture/workspace/.kodex'
status=0; prepare >"$fixture/foreign.log" 2>&1 || status=$?
[[ "$status" == 1 ]]
printf 'Workspace nested mount preparation and guarded materialization passed\n'
