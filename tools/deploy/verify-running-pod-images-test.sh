#!/usr/bin/env bash
set -euo pipefail

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
verifier="$script_directory/verify-running-pod-images.jq"
digest_a="sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
digest_b="sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

fixture() {
  local phase=$1 ready=$2 requested_digest=$3 running_digest=$4
  jq -n \
    --arg phase "$phase" \
    --argjson ready "$ready" \
    --arg requested_digest "$requested_digest" \
    --arg running_digest "$running_digest" '
      {
        metadata:{name:"current"},
        spec:{containers:[{name:"application",image:("localhost:5001/kodex/control-plane@" + $requested_digest)}]},
        status:{phase:$phase,containerStatuses:[{
          name:"application",ready:$ready,
          imageID:("localhost:5001/kodex/control-plane@" + $running_digest)
        }]}
      }
    '
}

current_ready=$(fixture Running true "$digest_a" "$digest_a")
historical_failed=$(fixture Failed false "$digest_b" "$digest_b")
jq -n --argjson current "$current_ready" --argjson historical "$historical_failed" \
  '{items:[$current,$historical]}' | jq -e -f "$verifier" >/dev/null

current_mismatch=$(fixture Running true "$digest_a" "$digest_b")
if jq -n --argjson pod "$current_mismatch" '{items:[$pod]}' | jq -e -f "$verifier" >/dev/null; then
  printf 'mismatched running digest was accepted\n' >&2
  exit 1
fi

current_unready=$(fixture Running false "$digest_a" "$digest_a")
if jq -n --argjson pod "$current_unready" '{items:[$pod]}' | jq -e -f "$verifier" >/dev/null; then
  printf 'unready running container was accepted\n' >&2
  exit 1
fi

zero_digest="sha256:0000000000000000000000000000000000000000000000000000000000000000"
current_zero=$(fixture Running true "$zero_digest" "$zero_digest")
if jq -n --argjson pod "$current_zero" '{items:[$pod]}' | jq -e -f "$verifier" >/dev/null; then
  printf 'zero running digest was accepted\n' >&2
  exit 1
fi

if jq -n --argjson pod "$historical_failed" '{items:[$pod]}' | jq -e -f "$verifier" >/dev/null; then
  printf 'pod set without a running workload was accepted\n' >&2
  exit 1
fi

printf 'Running pod image verification tests passed\n'
