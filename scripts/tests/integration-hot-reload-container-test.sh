#!/usr/bin/env bash
set -euo pipefail

# Локальный exact image; без сети, provider credentials и данных окружения.
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
image=${KODEX_INTEGRATION_RUNTIME_TEST_IMAGE:?KODEX_INTEGRATION_RUNTIME_TEST_IMAGE is required}
[[ "$image" =~ ^sha256:[a-f0-9]{64}$ ]] || exit 1
docker image inspect "$image" >/dev/null
timeout 30s docker run --rm --pull=never --network none \
  --read-only --user 10001:10001 --cap-drop ALL --security-opt no-new-privileges \
  --mount "type=bind,src=$root,dst=/workspace,readonly" \
  --tmpfs /tmp:rw,nosuid,nodev,size=67108864,mode=1777 \
  --entrypoint /bin/sh "$image" -ec '
    test "$(go env GOVERSION)" = go1.26.6
    test "$(/usr/bin/git --version)" = "git version 2.52.0"
    test -s /etc/ssl/certs/ca-certificates.crt
    test -x /usr/libexec/git-core/git-remote-https
    directory=$(mktemp -d /tmp/kodex-writeback-test-XXXXXX)
    trap '\''rm -rf -- "$directory"'\'' EXIT
    test "$(stat -c %a "$directory")" = 700
    GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null \
      git -C "$directory" -c core.hooksPath=/dev/null init --bare --quiet .
    test -d "$directory/objects"
    test "$(git -C "$directory" rev-parse --is-bare-repository)" = true
    if touch /workspace/.integration-write-forbidden 2>/dev/null; then exit 1; fi
  '
printf 'Integration hot-reload Git runtime and storage checks passed\n'
