#!/usr/bin/env bash
set -euo pipefail

render=${1:?render path is required}
stt_image=${2:?exact STT image is required}
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
targets=$(yq -r '
  select(.kind == "ConfigMap" and .metadata.name == "internal-rpc-authority-publisher-target-registry") |
  .data."key-delivery-targets.yaml"
' "$render" | yq -o=json -I=0 '.targets' | jq '[.[] | select(.workload_id == "stt-tts-service")]')
yq -o=json -I=0 '.' "$render" | jq -s -e \
  --arg image "$stt_image" --argjson targets "${targets:-[]}" \
  -f "$root/tools/dev/verify-local-stt-render.jq" >/dev/null || {
    printf 'Local STT runtime, authority or egress render is incomplete\n' >&2
    exit 1
  }
