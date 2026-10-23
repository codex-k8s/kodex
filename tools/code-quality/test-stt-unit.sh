#!/usr/bin/env bash
set -euo pipefail

# Отдельный target не запускает сервис и не меняет финальный runtime-образ.
task_root=$(git -C "$(dirname "$0")" rev-parse --show-toplevel)
module_root="$task_root/services/internal/stt-tts-service"
module_cache=$(GOENV=off GOTOOLCHAIN=go1.26.6 go env GOMODCACHE)
unit_cache=${KODEX_CQ_STT_CACHE:-$(mktemp -d /tmp/kodex-cq1823-stt-cache-XXXXXX)}
build_context=$(mktemp -d /tmp/kodex-cq1823-stt-image-XXXXXX)
trap 'rm -rf -- "$build_context"' EXIT

# Кэш не может подменить произвольную часть хоста в writable mount.
[[ "$unit_cache" == /tmp/kodex-cq1823-stt-cache-* && "${unit_cache%/*}" == /tmp &&
   -d "$unit_cache" && -O "$unit_cache" && ! -L "$unit_cache" &&
   $(stat -c '%a' "$unit_cache") == 700 ]] || {
  printf '%s\n' 'STT unit cache must be a private task-owned temporary directory' >&2
  exit 1
}
[[ -d "$module_cache" && -d "$module_root" ]] || {
  printf '%s\n' 'STT source and prepopulated Go module cache are required' >&2
  exit 1
}

docker build --target unit-test \
  --file "$task_root/tools/dev/Dockerfile.local-stt" \
  --iidfile "$build_context/image-id" "$build_context"
image_id=$(<"$build_context/image-id")
[[ "$image_id" =~ ^sha256:[a-f0-9]{64}$ ]] || {
  printf '%s\n' 'STT unit image ID is invalid' >&2
  exit 1
}
printf 'STT unit image: %s\nSTT unit cache: %s\n' "$image_id" "$unit_cache"

# В контейнер не передаются env хоста, корневые .env/.git и другие сервисы.
# Доступна только loopback-сеть для синтетических RPC/HTTP тестов.
docker run --rm --network none --user "$(id -u):$(id -g)" \
  --cap-drop ALL --security-opt no-new-privileges --read-only \
  --cpus 4 --memory 4g --pids-limit 512 \
  --tmpfs /tmp:rw,exec,size=512m,mode=1777 \
  --mount "type=bind,src=$task_root/libs,dst=/workspace/libs,readonly" \
  --mount "type=bind,src=$module_root,dst=/workspace/services/internal/stt-tts-service,readonly" \
  --mount "type=bind,src=$task_root/deploy/k8s/base/internal-rpc-authority-publisher/authority-policy.json,dst=/workspace/deploy/k8s/base/internal-rpc-authority-publisher/authority-policy.json,readonly" \
  --mount "type=bind,src=$task_root/deploy/k8s/base/stt-tts-service/deployment.yaml,dst=/workspace/deploy/k8s/base/stt-tts-service/deployment.yaml,readonly" \
  --mount "type=bind,src=$module_cache,dst=/go/pkg/mod,readonly" \
  --mount "type=bind,src=$unit_cache,dst=/cache" \
  --env GOCACHE=/cache --env GOENV=off --env GOWORK=off \
  --env GOTOOLCHAIN=local --env GOPROXY=off --env GOSUMDB=off \
  --env CGO_ENABLED=0 --entrypoint /bin/sh "$image_id" -ec '
    go version && ffmpeg -version >/dev/null &&
    go test -count=1 ./... && go build ./... &&
    go test -count=1 -v -run "TestLiveProviderRussianNumberFixture|TestRealMediaRecorderContainers" \
      ./internal/providersmoke ./internal/integration/audio/ffmpeg
  '
