#!/usr/bin/env bash
set -euo pipefail

cache_root=${1:?primed local Go cache root is required}
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
image=docker.io/library/golang:1.26.6-alpine@sha256:3889b425f035be855a72fb4755265311293b6d414521f0a519d819df32222d83
air_sha=$(sha256sum "$cache_root/go-tools/air" | awk '{print $1}')
common=(--rm --network none --read-only --cap-drop ALL --security-opt no-new-privileges
  --mount "type=bind,src=$root,dst=/workspace,readonly"
  --mount "type=bind,src=$cache_root/go-mod-v2,dst=/go/pkg/mod,readonly"
  --mount "type=bind,src=$cache_root/go-sumdb,dst=/go/pkg/sumdb,readonly"
  --mount "type=bind,src=$cache_root/go-tools,dst=/go/tools,readonly"
  --tmpfs '/go/build-cache:rw,exec,mode=0777'
  -e GOMODCACHE=/go/pkg/mod -e GOCACHE=/go/build-cache/cache -e GOPATH=/go
  -e GOTOOLCHAIN=local -e GOWORK=off -e GOPROXY=off -e CGO_ENABLED=0
  -e GOTMPDIR=/go/build-cache/tmp -e HOME=/go/build-cache/home)

# Тот же init entrypoint, что в render; capabilities и root отсутствуют.
docker run "${common[@]}" --user 29000:29000 \
  --tmpfs /tmp:rw,exec,uid=29000,gid=29000,mode=0700 \
  --tmpfs /usr/local/bin:rw,exec,uid=29000,gid=29000,mode=0700 \
  --tmpfs /run/kodex:rw,exec,uid=29000,gid=29000,mode=0770 \
  "$image" sh -ec '
    timeout 180s /workspace/tools/dev/run-authority-socket-init.sh
    test -x /run/kodex/internal-rpc-authority/local-readiness
    test "$(stat -c %u:%g /run/kodex/internal-rpc-authority)" = 29000:29000
  '
printf 'Local EMAIL non-root socket init passed\n'

# Новый контейнер получает отдельный tmpfs cache, как отдельный cache в render.
docker run "${common[@]}" --user 10001:10001 \
  --tmpfs /tmp:rw,exec,uid=10001,gid=10001,mode=0700 \
  -e "KODEX_DEV_AIR_SHA256=$air_sha" "$image" sh -ec '
    mkdir -p "$GOCACHE" "$GOTMPDIR" "$HOME"
    cd /workspace/services/internal/email-bridge
    timeout 180s go build -trimpath -buildvcs=false -o /tmp/email-cli ./cmd/cli
    # Без DSN команда должна отказать, не выполнять скрытую сетевую попытку.
    if timeout 30s /workspace/tools/dev/run-go-command.sh services/internal/email-bridge ./cmd/cli up >/tmp/migration.log 2>&1; then
      exit 1
    fi
    grep -q "Email bridge migration failed" /tmp/migration.log
    printf "Local EMAIL offline migration rejection passed\n"
    /workspace/tools/dev/run-go-hot-reload.sh services/internal/email-bridge ./cmd/email-bridge email-bridge >/tmp/air.log 2>&1 &
    pid=$!
    trap "kill $pid 2>/dev/null || true; wait $pid 2>/dev/null || true" EXIT
    remaining=180
    while test ! -x /tmp/kodex-dev-email-bridge/build/main; do
      kill -0 "$pid"
      remaining=$((remaining - 1))
      test "$remaining" -gt 0
      sleep 1
    done
    test ! -w /workspace/go.work
    test ! -w /go/pkg/mod
    test ! -w /go/tools/air
  '
printf 'Local EMAIL process contract passed: offline non-root socket init, migration rejection and Air build\n'
