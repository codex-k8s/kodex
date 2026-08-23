#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'NATS application account configuration failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf 'Usage: %s --nsc-home <path>\n' "$0" >&2
}

nsc_home=""
while (($# > 0)); do
  case "$1" in
    --nsc-home) nsc_home="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -d "$nsc_home" && ! -L "$nsc_home" ]] || fail 'nsc home is invalid'
for command_name in jq mktemp nsc; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done

readonly account_name=APPLICATION
readonly memory_storage_bytes=268435456
readonly disk_storage_bytes=34359738368
readonly maximum_streams=8
readonly maximum_consumers=64
readonly maximum_ack_pending=100000

nsc -H "$nsc_home" edit account --name "$account_name" \
  --js-mem-storage "$memory_storage_bytes" \
  --js-disk-storage "$disk_storage_bytes" \
  --js-streams "$maximum_streams" \
  --js-consumer "$maximum_consumers" \
  --js-max-ack-pending "$maximum_ack_pending" \
  --js-max-mem-stream "$memory_storage_bytes" \
  --js-max-disk-stream "$disk_storage_bytes" \
  --js-max-bytes-required >/dev/null 2>&1 || fail 'nsc account edit failed'

readback_file=$(mktemp)
trap 'rm -f -- "$readback_file"' EXIT
nsc -H "$nsc_home" describe account --name "$account_name" --json \
  --output-file "$readback_file" >/dev/null 2>&1 || fail 'nsc account readback failed'

jq -e \
  --argjson memory_storage "$memory_storage_bytes" \
  --argjson disk_storage "$disk_storage_bytes" \
  --argjson streams "$maximum_streams" \
  --argjson consumers "$maximum_consumers" \
  --argjson ack_pending "$maximum_ack_pending" '
    .name == "APPLICATION" and
    .nats.type == "account" and
    .nats.limits.mem_storage == $memory_storage and
    .nats.limits.disk_storage == $disk_storage and
    .nats.limits.streams == $streams and
    .nats.limits.consumer == $consumers and
    .nats.limits.max_ack_pending == $ack_pending and
    .nats.limits.mem_max_stream_bytes == $memory_storage and
    .nats.limits.disk_max_stream_bytes == $disk_storage and
    .nats.limits.max_bytes_required == true
  ' "$readback_file" >/dev/null || fail 'JetStream account limits readback mismatch'

printf 'NATS application account configured with bounded JetStream limits\n'
