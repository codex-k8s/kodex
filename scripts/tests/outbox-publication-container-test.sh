#!/usr/bin/env bash
set -euo pipefail

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
image=${KODEX_OUTBOX_TEST_IMAGE:?KODEX_OUTBOX_TEST_IMAGE is required}
[[ "$image" =~ ^sha256:[a-f0-9]{64}$ ]] || exit 1
[[ ( -z "${DOCKER_HOST:-}" || "$DOCKER_HOST" == unix:///* ) &&
   "$(docker context inspect --format '{{.Endpoints.docker.Host}}')" == unix:///* &&
   "$(docker info --format '{{.OSType}}')" == linux ]] || exit 1
docker image inspect "$image" >/dev/null
fixture=$(mktemp -d /tmp/kodex-outbox-publication.XXXXXXXX)
cleanup() {
  timeout 20s docker run --rm --pull=never --network none --read-only --user 0:0 \
    --cap-drop ALL --cap-add DAC_OVERRIDE --security-opt no-new-privileges --entrypoint /bin/sh \
    --mount "type=bind,src=$fixture,dst=/fixture" "$image" \
    -ec 'rm -rf /fixture/workspace' >/dev/null 2>&1 || return 1
  rm -f -- "$fixture/codex.test" "$fixture/app.test"
  rmdir -- "$fixture"
}
trap cleanup EXIT
chmod 0755 "$fixture"
CGO_ENABLED=0 GOWORK=off timeout 120s go -C "$repository_root/services/jobs/agent-runner" test -c -o "$fixture/codex.test" ./internal/codex
CGO_ENABLED=0 GOWORK=off timeout 120s go -C "$repository_root/services/jobs/agent-runner" test -c -o "$fixture/app.test" ./internal/app
chmod 0555 "$fixture/codex.test" "$fixture/app.test"
timeout 20s docker run --rm --pull=never --network none --read-only --user 0:0 \
  --cap-drop ALL --cap-add CHOWN --cap-add DAC_OVERRIDE --security-opt no-new-privileges --entrypoint /bin/sh \
  --mount "type=bind,src=$fixture,dst=/fixture" "$image" -ec '
    mkdir -p /fixture/workspace/.kodex/outbox
    chmod 2770 /fixture/workspace /fixture/workspace/.kodex /fixture/workspace/.kodex/outbox
    chown 10001:29000 /fixture/workspace /fixture/workspace/.kodex /fixture/workspace/.kodex/outbox'
fixture_test() {
  local identity=$1 executable=$2 mode=$3 test_name=$4
  timeout 20s docker run --rm --pull=never --network none --read-only \
    --user "$identity:$identity" --group-add 29000 --cap-drop ALL --security-opt no-new-privileges \
    --mount "type=bind,src=$fixture/workspace,dst=/workspace" \
    --mount "type=bind,src=$fixture/$executable,dst=/fixture.test,readonly" \
    -e "KODEX_OUTBOX_PUBLICATION_TEST=$mode" --entrypoint /fixture.test "$image" \
    -test.run "^${test_name}$" -test.timeout 10s
}
fixture_test 10002 codex.test WRITE TestProviderOutboxPublicationContainer
fixture_test 10001 app.test UNPUBLISHED TestRunnerOutboxPublicationContainer
fixture_test 10002 codex.test PUBLISH TestProviderOutboxPublicationContainer
fixture_test 10001 app.test COLLECT TestRunnerOutboxPublicationContainer
timeout 20s docker run --rm --pull=never --network none --read-only --user 10001:10001 \
  --group-add 29000 --cap-drop ALL --security-opt no-new-privileges --entrypoint /bin/sh \
  --mount "type=bind,src=$fixture/workspace,dst=/workspace" "$image" \
  -ec 'printf synthetic > /workspace/.kodex/outbox/foreign.md; chmod 0640 /workspace/.kodex/outbox/foreign.md'
fixture_test 10002 codex.test REJECT_FOREIGN TestProviderOutboxPublicationContainer
printf 'Dual-UID private and atomic outbox publication, exact collection and foreign-owner rejection passed\n'
