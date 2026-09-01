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
    '  [--http-timeout-seconds <1-99>]' >&2
}

hosts_raw=""
allowed_ipv4_raw=""
allowed_ipv6_raw=""
dns_timeout_seconds=10
http_timeout_seconds=10
while (($# > 0)); do
  case "$1" in
    --hosts) hosts_raw=${2:-}; shift 2 ;;
    --allowed-ipv4-addresses) allowed_ipv4_raw=${2:-}; shift 2 ;;
    --allowed-ipv6-addresses) allowed_ipv6_raw=${2:-}; shift 2 ;;
    --dns-timeout-seconds) dns_timeout_seconds=${2:-}; shift 2 ;;
    --http-timeout-seconds) http_timeout_seconds=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ "$dns_timeout_seconds" =~ ^[1-9][0-9]?$ ]] ||
  fail 'DNS timeout must be between 1 and 99 seconds'
[[ "$http_timeout_seconds" =~ ^[1-9][0-9]?$ ]] ||
  fail 'HTTP timeout must be between 1 and 99 seconds'
for command_name in curl dig python3 timeout; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done

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
    http_code=$(timeout "${http_timeout_seconds}s" curl --silent --show-error \
      --output /dev/null --write-out '%{http_code}' --noproxy '*' \
      --connect-timeout "$http_timeout_seconds" --max-time "$http_timeout_seconds" \
      --resolve "$host:80:$address" --header "Host: $host" \
      "http://$host/.well-known/acme-challenge/kodex-preflight-$$-$RANDOM") ||
      fail "HTTP-01 endpoint is unreachable: $host/$address"
    [[ "$http_code" =~ ^[1-4][0-9]{2}$ ]] ||
      fail "HTTP-01 endpoint returned an invalid status: $host/$address/$http_code"
  done
  for address in "${host_ipv6[@]}"; do
    printf '%s\n' "${allowed_ipv6[@]}" | grep -Fxq -- "$address" ||
      fail "DNS host resolves to an unauthorized IPv6 address: $host/$address"
    observed_ipv6+=("$address")
    http_code=$(timeout "${http_timeout_seconds}s" curl --silent --show-error \
      --output /dev/null --write-out '%{http_code}' --noproxy '*' \
      --connect-timeout "$http_timeout_seconds" --max-time "$http_timeout_seconds" \
      --resolve "$host:80:[$address]" --header "Host: $host" \
      "http://$host/.well-known/acme-challenge/kodex-preflight-$$-$RANDOM") ||
      fail "HTTP-01 endpoint is unreachable: $host/$address"
    [[ "$http_code" =~ ^[1-4][0-9]{2}$ ]] ||
      fail "HTTP-01 endpoint returned an invalid status: $host/$address/$http_code"
  done
done

[[ "$(printf '%s\n' "${observed_ipv4[@]}" | sed '/^$/d' | sort -u)" == "$allowed_ipv4_output" ]] ||
  fail 'allowed IPv4 addresses differ from the DNS snapshot'
[[ "$(printf '%s\n' "${observed_ipv6[@]}" | sed '/^$/d' | sort -u)" == "$allowed_ipv6_output" ]] ||
  fail 'allowed IPv6 addresses differ from the DNS snapshot'
printf 'Kodex public development endpoint preflight completed: hosts=%s\n' "${#hosts[@]}"
