#!/usr/bin/env sh
set -eu

generated_dir=services/external/control-api-gateway/internal/transport/websocket/generated

if find "$generated_dir" -maxdepth 1 -type f -iname 'anonymous_schema_*.go' | grep -q .; then
  echo "anonymous AsyncAPI model file is forbidden" >&2
  exit 1
fi
if rg -q 'AnonymousSchema' "$generated_dir"; then
  echo "anonymous AsyncAPI model symbol is forbidden" >&2
  exit 1
fi
if rg -q 'func \([^)]*\) (UnmarshalJSON|MarshalJSON)\(' "$generated_dir"; then
  echo "generated AsyncAPI JSON codec is forbidden; use the strict runtime boundary" >&2
  exit 1
fi
for required in \
  subscribe_envelope.go subscribe_message_type.go projection_channel.go \
  resource_kind.go snapshot_envelope.go snapshot_message_type.go \
  snapshot_items.go problem_envelope.go problem_message_type.go \
  resource.go run_projection.go incident_projection.go configuration_change.go; do
  if [ ! -f "$generated_dir/$required" ]; then
    echo "named AsyncAPI model is missing: $required" >&2
    exit 1
  fi
done
