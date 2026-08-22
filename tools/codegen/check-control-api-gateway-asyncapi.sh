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
  resume_envelope.go snapshot_envelope.go event_envelope.go \
  resync_envelope.go heartbeat_envelope.go problem_envelope.go \
  run_graph.go run_node.go run_node_type.go run_node_state.go \
  run_edge.go run_edge_type.go run_event.go run_event_type.go run_state.go \
  next_action.go resync_reason.go problem_code.go; do
  if [ ! -f "$generated_dir/$required" ]; then
    echo "named AsyncAPI model is missing: $required" >&2
    exit 1
  fi
done
