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
# Modelina недетерминированно выбирает одно из двух эквивалентных enum-имён
# ResourceKind для configuration change. Фиксируем локальный тип события, чтобы
# повторная генерация не создавала ложный generated diff.
generated_dir=services/external/control-api-gateway/internal/transport/websocket/generated
configuration_change=
for candidate in "$generated_dir"/anonymous_schema_*.go; do
  if grep -q 'json:"action" binding:"required"' "$candidate" &&
    grep -q 'json:"resourceKind" binding:"required"' "$candidate" &&
    grep -q 'json:"outcome" binding:"required"' "$candidate"; then
    configuration_change=$candidate
    break
  fi
done
resource_kind_file=$(grep -El '"PROJECT",[[:space:]]*"TEAM",[[:space:]]*"CHAT",[[:space:]]*"ROLE"' \
  "$generated_dir"/anonymous_schema_*.go | sort -V | tail -1)
resource_kind_type=$(sed -n 's/^type \(AnonymousSchema_[0-9][0-9]*\) uint$/\1/p' "$resource_kind_file")
if [ -z "$configuration_change" ] || [ -z "$resource_kind_type" ]; then
  echo "generated AsyncAPI configuration change model is missing" >&2
  exit 1
fi
sed -i 's/\*AnonymousSchema_[0-9][0-9]* `json:"resourceKind"/\*'"$resource_kind_type"' `json:"resourceKind"/' "$configuration_change"
snapshot_items="$generated_dir/snapshot_items.go"
if [ ! -f "$snapshot_items" ]; then
  echo "generated AsyncAPI snapshot items model is missing" >&2
  exit 1
fi
cp tools/codegen/templates/control-api-gateway-asyncapi/snapshot_items_contract.go \
  "$generated_dir/snapshot_items_contract.go"
gofmt -w "$generated_dir"
