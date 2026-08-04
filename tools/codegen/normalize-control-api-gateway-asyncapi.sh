#!/usr/bin/env sh
set -eu

target=services/external/control-api-gateway/internal/transport/websocket/generated/realtime.go
if [ ! -f "$target" ]; then
  echo "generated AsyncAPI realtime model is missing" >&2
  exit 1
fi
sed -i 's/json:"-,omitempty`/json:"-"`/g' "$target"
if grep -q 'json:"-,omitempty`' "$target"; then
  echo "generated AsyncAPI model normalization failed" >&2
  exit 1
fi
gofmt -w services/external/control-api-gateway/internal/transport/websocket/generated
