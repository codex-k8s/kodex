#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex public development endpoint preflight failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 --hosts <comma-separated-DNS> --allowed-ipv4-addresses <comma-list>" \
    '  [--allowed-ipv6-addresses <comma-list>] [--dns-timeout-seconds <1-99>]' \
    '  [--http-timeout-seconds <1-99>] --context <exact-context>' \
    '  --backend-address <private-IPv4>' >&2
}

hosts_raw=""
allowed_ipv4_raw=""
allowed_ipv6_raw=""
dns_timeout_seconds=10
http_timeout_seconds=10
context=""
backend_address=""
while (($# > 0)); do
  case "$1" in
    --hosts) hosts_raw=${2:-}; shift 2 ;;
    --allowed-ipv4-addresses) allowed_ipv4_raw=${2:-}; shift 2 ;;
    --allowed-ipv6-addresses) allowed_ipv6_raw=${2:-}; shift 2 ;;
    --dns-timeout-seconds) dns_timeout_seconds=${2:-}; shift 2 ;;
    --http-timeout-seconds) http_timeout_seconds=${2:-}; shift 2 ;;
    --context) context=${2:-}; shift 2 ;;
    --backend-address) backend_address=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ "$dns_timeout_seconds" =~ ^[1-9][0-9]?$ ]] ||
  fail 'DNS timeout must be between 1 and 99 seconds'
[[ "$http_timeout_seconds" =~ ^[1-9][0-9]?$ ]] ||
  fail 'HTTP timeout must be between 1 and 99 seconds'
for command_name in curl dig jq kubectl python3 timeout; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ -n "$context" && "$(kubectl config current-context)" == "$context" ]] ||
  fail 'Kubernetes context mismatch'
[[ "${context,,}" != *prod* && "${context,,}" != *production* ]] ||
  fail 'production context is forbidden'
python3 - "$backend_address" <<'PY' || fail 'preflight backend address must be a private non-loopback IPv4 address'
import ipaddress
import sys

address = ipaddress.ip_address(sys.argv[1])
if address.version != 4 or not address.is_private or address.is_loopback or address.is_unspecified:
    raise SystemExit(1)
PY

canonical_address_list() {
  local family=$1 raw=$2
  python3 - "$family" "$raw" <<'PY'
import ipaddress
import sys

family = int(sys.argv[1])
raw = sys.argv[2]
values = [] if raw == "" else raw.split(",")
canonical = []
for value in values:
    if value != value.strip() or not value or any(marker in value for marker in ("*", "/", "%")):
        raise SystemExit(1)
    address = ipaddress.ip_address(value)
    if address.version != family:
        raise SystemExit(1)
    canonical.append(str(address))
if len(canonical) != len(set(canonical)):
    raise SystemExit(1)
print("\n".join(sorted(canonical)))
PY
}

canonical_dns_addresses() {
  local family=$1
  python3 -c '
import ipaddress
import sys

family = int(sys.argv[1])
result = set()
for line in sys.stdin:
    value = line.strip().rstrip(".")
    if not value:
        continue
    try:
        address = ipaddress.ip_address(value)
    except ValueError:
        continue
    if address.version == family:
        result.add(str(address))
print("\n".join(sorted(result)))
' "$family"
}

allowed_ipv4_output=$(canonical_address_list 4 "$allowed_ipv4_raw") ||
  fail 'allowed IPv4 address list is invalid'
allowed_ipv6_output=$(canonical_address_list 6 "$allowed_ipv6_raw") ||
  fail 'allowed IPv6 address list is invalid'
[[ -n "$allowed_ipv4_output" || -n "$allowed_ipv6_output" ]] ||
  fail 'at least one allowed public address is required'
mapfile -t allowed_ipv4 < <(printf '%s\n' "$allowed_ipv4_output" | sed '/^$/d')
mapfile -t allowed_ipv6 < <(printf '%s\n' "$allowed_ipv6_output" | sed '/^$/d')

IFS=',' read -r -a hosts <<<"$hosts_raw"
((${#hosts[@]} > 0)) || fail 'at least one public DNS host is required'
declare -A seen_hosts=()
declare -a observed_ipv4=() observed_ipv6=()
for host in "${hosts[@]}"; do
  [[ "$host" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ && "$host" == *.* &&
    "$host" != *..* && -z "${seen_hosts[$host]:-}" ]] ||
    fail "public DNS host is invalid or duplicated: $host"
  seen_hosts[$host]=true
done

preflight_namespace=kodex-acme-preflight
preflight_name="kodex-acme-preflight-$$-$RANDOM"
challenge_token="$preflight_name-$RANDOM"
challenge_path="/.well-known/acme-challenge/$challenge_token"
preflight_port=18080
temporary_directory=$(mktemp -d)
responder_pid=""
cleanup() {
  if [[ -n "$responder_pid" ]]; then
    kill "$responder_pid" >/dev/null 2>&1 || true
    wait "$responder_pid" >/dev/null 2>&1 || true
  fi
  kubectl -n "$preflight_namespace" delete ingress "$preflight_name" \
    --ignore-not-found --wait=true --timeout=1m >/dev/null 2>&1 || true
  kubectl -n "$preflight_namespace" delete endpointslice "$preflight_name" \
    --ignore-not-found --wait=true --timeout=1m >/dev/null 2>&1 || true
  kubectl -n "$preflight_namespace" delete service "$preflight_name" \
    --ignore-not-found --wait=true --timeout=1m >/dev/null 2>&1 || true
  rm -rf -- "$temporary_directory"
}
trap cleanup EXIT

verify_external_http01() {
  local host=$1 submission probe_id result status
  submission=$(jq -cn --arg domain "$host" --arg path "$challenge_token" \
    --arg expected "$challenge_token" '{method:"http-01",domain:$domain,
      options:{http_request_path:$path,http_expect_response:$expected}}' |
    curl --fail --silent --show-error --max-time 20 \
      -H 'content-type: application/json' --data-binary @- https://letsdebug.net) ||
    fail "external HTTP-01 probe could not be submitted: $host"
  probe_id=$(jq -er --arg host "$host" '
    select(.Domain == $host) | .ID |
    select(type == "number" and . > 0 and floor == .)
  ' <<<"$submission") || fail "external HTTP-01 probe response is invalid: $host"
  for _ in $(seq 1 90); do
    result=$(curl --fail --silent --show-error --max-time 10 \
      -H 'accept: application/json' "https://letsdebug.net/$host/$probe_id") ||
      fail "external HTTP-01 probe readback failed: $host"
    status=$(jq -r '.status // ""' <<<"$result")
    if [[ "$status" == Complete ]]; then
      jq -e --arg host "$host" --argjson id "$probe_id" '
        .domain == $host and .id == $id and .method == "http-01" and
        .status == "Complete" and .result.ok == true and
        ([.result.problems[]? | select(.severity == "Error" or .severity == "Fatal")] | length) == 0
      ' <<<"$result" >/dev/null || fail "external HTTP-01 route verification failed: $host"
      return
    fi
    [[ "$status" == Pending || "$status" == Processing || -z "$status" ]] ||
      fail "external HTTP-01 probe entered an unexpected state: $host"
    sleep 1
  done
  fail "external HTTP-01 probe timed out: $host"
}

verify_external_https_port() {
  local host=$1 submission request_id result successful
  submission=$(curl --fail --silent --show-error --max-time 20 \
    -H 'accept: application/json' --get \
    --data-urlencode "host=$host:443" --data-urlencode 'max_nodes=3' \
    https://check-host.net/check-tcp) ||
    fail "external HTTPS port probe could not be submitted: $host"
  request_id=$(jq -er '
    select(.ok == 1 and (.nodes | type == "object") and (.nodes | length) >= 1) |
    .request_id | select(type == "string" and test("^[A-Za-z0-9_-]+$"))
  ' <<<"$submission") || fail "external HTTPS port probe response is invalid: $host"
  for _ in $(seq 1 30); do
    result=$(curl --fail --silent --show-error --max-time 10 \
      -H 'accept: application/json' "https://check-host.net/check-result/$request_id") ||
      fail "external HTTPS port probe readback failed: $host"
    successful=$(jq '[to_entries[] | .value[]? |
      select(type == "object" and has("time") and (.time | type == "number"))] | length' \
      <<<"$result") || fail "external HTTPS port probe result is invalid: $host"
    if ((successful >= 1)); then
      return
    fi
    sleep 1
  done
  fail "external HTTPS port is unreachable: $host"
}

install -d -m 0700 "$temporary_directory/.well-known/acme-challenge"
printf '%s' "$challenge_token" >"$temporary_directory$challenge_path"
python3 -m http.server "$preflight_port" --bind "$backend_address" \
  --directory "$temporary_directory" >"$temporary_directory/responder.log" 2>&1 &
responder_pid=$!
for _ in $(seq 1 30); do
  kill -0 "$responder_pid" >/dev/null 2>&1 || fail 'ACME preflight responder terminated unexpectedly'
  if curl --fail --silent --show-error --noproxy '*' \
    --resolve "preflight.local:$preflight_port:$backend_address" \
    "http://preflight.local:$preflight_port$challenge_path" | grep -Fxq "$challenge_token"; then
    break
  fi
  sleep 1
done
curl --fail --silent --show-error --noproxy '*' \
  --resolve "preflight.local:$preflight_port:$backend_address" \
  "http://preflight.local:$preflight_port$challenge_path" | grep -Fxq "$challenge_token" ||
  fail 'ACME preflight responder is unavailable'

hosts_json=$(printf '%s\n' "${hosts[@]}" | jq -R . | jq -s .)
kubectl create namespace "$preflight_namespace" --dry-run=client -o yaml |
  kubectl apply --server-side --field-manager=kodex-acme-preflight -f - >/dev/null
jq -n --arg namespace "$preflight_namespace" --arg name "$preflight_name" '
  {
    apiVersion:"v1",kind:"Service",metadata:{namespace:$namespace,name:$name,
      labels:{"app.kubernetes.io/part-of":"kodex-acme-preflight"}},
    spec:{ports:[{name:"http",protocol:"TCP",port:80,targetPort:18080}]}
  }
' | kubectl apply --server-side --field-manager=kodex-acme-preflight -f - >/dev/null
jq -n --arg namespace "$preflight_namespace" --arg name "$preflight_name" \
  --arg backend "$backend_address" '
  {
    apiVersion:"discovery.k8s.io/v1",kind:"EndpointSlice",
    metadata:{namespace:$namespace,name:$name,
      labels:{"kubernetes.io/service-name":$name,"app.kubernetes.io/part-of":"kodex-acme-preflight"}},
    addressType:"IPv4",ports:[{name:"http",protocol:"TCP",port:18080}],
    endpoints:[{addresses:[$backend],conditions:{ready:true}}]
  }
' | kubectl apply --server-side --field-manager=kodex-acme-preflight -f - >/dev/null
jq -n --arg namespace "$preflight_namespace" --arg name "$preflight_name" \
  --arg path "$challenge_path" --argjson hosts "$hosts_json" '
  {
    apiVersion:"networking.k8s.io/v1",kind:"Ingress",
    metadata:{namespace:$namespace,name:$name,
      labels:{"app.kubernetes.io/part-of":"kodex-acme-preflight"},
      annotations:{"traefik.ingress.kubernetes.io/router.entrypoints":"web"}},
    spec:{ingressClassName:"traefik",rules:($hosts | map({host:.,http:{paths:[{
      path:$path,pathType:"Exact",backend:{service:{name:$name,port:{number:80}}}
    }]}}))}
  }
' | kubectl apply --server-side --field-manager=kodex-acme-preflight -f - >/dev/null

declare -A checked_https_addresses=()
for host in "${hosts[@]}"; do
  dns_output=$(timeout "$((dns_timeout_seconds + 2))s" \
    dig +time="$dns_timeout_seconds" +tries=1 +short A "$host") ||
    fail "DNS A lookup failed: $host"
  mapfile -t host_ipv4 < <(canonical_dns_addresses 4 <<<"$dns_output" | sed '/^$/d')
  dns_output=$(timeout "$((dns_timeout_seconds + 2))s" \
    dig +time="$dns_timeout_seconds" +tries=1 +short AAAA "$host") ||
    fail "DNS AAAA lookup failed: $host"
  mapfile -t host_ipv6 < <(canonical_dns_addresses 6 <<<"$dns_output" | sed '/^$/d')
  ((${#host_ipv4[@]} + ${#host_ipv6[@]} > 0)) || fail "DNS host has no address: $host"

  for address in "${host_ipv4[@]}"; do
    printf '%s\n' "${allowed_ipv4[@]}" | grep -Fxq -- "$address" ||
      fail "DNS host resolves to an unauthorized IPv4 address: $host/$address"
    observed_ipv4+=("$address")
    challenge_verified=false
    for _ in $(seq 1 "$http_timeout_seconds"); do
      if body=$(curl --fail --silent --show-error --noproxy '*' \
        --connect-timeout 2 --max-time 3 --resolve "$host:80:$address" \
        "http://$host$challenge_path" 2>/dev/null) && [[ "$body" == "$challenge_token" ]]; then
        challenge_verified=true
        break
      fi
      sleep 1
    done
    [[ "$challenge_verified" == true ]] || fail "exact HTTP-01 route is unreachable: $host/$address"
    if [[ -z "${checked_https_addresses[$address]:-}" ]]; then
      python3 - "$address" "$http_timeout_seconds" <<'PY' || fail "HTTPS endpoint is unreachable: $host/$address"
import socket
import sys

with socket.create_connection((sys.argv[1], 443), timeout=int(sys.argv[2])):
    pass
PY
      checked_https_addresses[$address]=true
    fi
  done
  for address in "${host_ipv6[@]}"; do
    printf '%s\n' "${allowed_ipv6[@]}" | grep -Fxq -- "$address" ||
      fail "DNS host resolves to an unauthorized IPv6 address: $host/$address"
    observed_ipv6+=("$address")
    challenge_verified=false
    for _ in $(seq 1 "$http_timeout_seconds"); do
      if body=$(curl --fail --silent --show-error --noproxy '*' \
        --connect-timeout 2 --max-time 3 --resolve "$host:80:[$address]" \
        "http://$host$challenge_path" 2>/dev/null) && [[ "$body" == "$challenge_token" ]]; then
        challenge_verified=true
        break
      fi
      sleep 1
    done
    [[ "$challenge_verified" == true ]] || fail "exact HTTP-01 route is unreachable: $host/$address"
    if [[ -z "${checked_https_addresses[$address]:-}" ]]; then
      python3 - "$address" "$http_timeout_seconds" <<'PY' || fail "HTTPS endpoint is unreachable: $host/$address"
import socket
import sys

with socket.create_connection((sys.argv[1], 443), timeout=int(sys.argv[2])):
    pass
PY
      checked_https_addresses[$address]=true
    fi
  done
  verify_external_http01 "$host"
  verify_external_https_port "$host"
done

[[ "$(printf '%s\n' "${observed_ipv4[@]}" | sed '/^$/d' | sort -u)" == "$allowed_ipv4_output" ]] ||
  fail 'allowed IPv4 addresses differ from the DNS snapshot'
[[ "$(printf '%s\n' "${observed_ipv6[@]}" | sed '/^$/d' | sort -u)" == "$allowed_ipv6_output" ]] ||
  fail 'allowed IPv6 addresses differ from the DNS snapshot'
printf 'Kodex public development endpoint preflight completed: hosts=%s\n' "${#hosts[@]}"
