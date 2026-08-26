#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex IPv6 ingress bridge configuration failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 --mode preflight|apply|readback [--server-public-ipv6-address <IPv6>]" >&2
}

mode=""
server_public_ipv6_address=""
while (($# > 0)); do
  case "$1" in
    --mode) mode="${2:-}"; shift 2 ;;
    --server-public-ipv6-address) server_public_ipv6_address="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

case "$mode" in preflight|apply|readback) ;; *) fail 'mode is invalid' ;; esac

test_root=${KODEX_INSTALL_TEST_ROOT:-}
if [[ -n "$test_root" ]]; then
  ((EUID != 0)) || fail 'test root is forbidden for root execution'
  [[ "$test_root" == /* && -d "$test_root" && ! -L "$test_root" ]] ||
    fail 'test root is invalid'
  systemd_unit_directory="$test_root/etc/systemd/system"
else
  ((EUID == 0)) || fail 'IPv6 ingress bridge configuration must run as root'
  systemd_unit_directory=/etc/systemd/system
fi

managed_marker='# Managed by Kodex installer: ipv6-ingress-bridge/v1'
connection_limit=256
unit_prefix=kodex-ipv6-ingress-bridge
ports=(80 443)
temporary_directory=""

cleanup() {
  [[ -z "$temporary_directory" ]] || rm -rf -- "$temporary_directory"
}
trap cleanup EXIT

unit_path() {
  printf '%s/%s-%s.%s' "$systemd_unit_directory" "$unit_prefix" "$1" "$2"
}

assert_unit_path_owned_or_absent() {
  local path=$1
  [[ ! -L "$path" ]] || fail "managed unit path is a symbolic link: $path"
  if [[ -e "$path" ]]; then
    [[ -f "$path" ]] || fail "managed unit path is not a regular file: $path"
    grep -Fxq "$managed_marker" "$path" || fail "unit is not owned by Kodex: $path"
  fi
}

assert_no_drop_ins() {
  local port=$1 kind=$2 directory
  directory="$(unit_path "$port" "$kind").d"
  [[ ! -e "$directory" && ! -L "$directory" ]] ||
    fail "unit drop-in path is not managed by Kodex: $directory"
}

assert_all_paths_safe() {
  local port kind path
  for port in "${ports[@]}"; do
    for kind in socket service; do
      path=$(unit_path "$port" "$kind")
      assert_unit_path_owned_or_absent "$path"
      assert_no_drop_ins "$port" "$kind"
    done
  done
}

normalize_ipv6_address() {
  local address=$1
  command -v python3 >/dev/null 2>&1 || fail 'python3 is required'
  python3 - "$address" <<'PY'
import ipaddress
import sys

try:
    address = ipaddress.IPv6Address(sys.argv[1])
except ipaddress.AddressValueError:
    raise SystemExit(1)
if not address.is_global or address.ipv4_mapped is not None:
    raise SystemExit(1)
print(address.compressed)
PY
}

assert_ipv6_address_on_host() {
  local address=$1 addresses
  command -v ip >/dev/null 2>&1 || fail 'ip is required'
  command -v jq >/dev/null 2>&1 || fail 'jq is required'
  addresses=$(ip -j -6 address show) || fail 'host IPv6 addresses are unavailable'
  jq -e --arg address "$address" '
    any(.[];
      any(.addr_info[]?;
        .family == "inet6" and .scope == "global" and .local == $address
      )
    )
  ' <<<"$addresses" >/dev/null || fail 'public IPv6 address is not assigned to the host'
}

resolve_socket_proxy_binary() {
  local binary="" candidate
  if [[ -n "$test_root" ]]; then
    binary=$(command -v systemd-socket-proxyd || true)
  else
    for candidate in \
      /usr/lib/systemd/systemd-socket-proxyd \
      /lib/systemd/systemd-socket-proxyd; do
      if [[ -x "$candidate" ]]; then
        binary=$candidate
        break
      fi
    done
  fi
  [[ -n "$binary" && "$binary" == /* && -x "$binary" ]] ||
    fail 'systemd-socket-proxyd is required'
  printf '%s' "$binary"
}

render_units() {
  local address=$1 proxy_binary=$2 output_directory=$3 port socket_file service_file
  install -d -m 0700 "$output_directory"
  for port in "${ports[@]}"; do
    socket_file="$output_directory/$unit_prefix-$port.socket"
    service_file="$output_directory/$unit_prefix-$port.service"
    cat >"$socket_file" <<EOF
$managed_marker
[Unit]
Description=Kodex exact IPv6 ingress bridge for TCP $port
Documentation=https://github.com/codex-k8s/kodex/blob/main/docs/runbooks/fresh-install.md

[Socket]
ListenStream=[$address]:$port
BindIPv6Only=ipv6-only
Accept=no
Service=$unit_prefix-$port.service
Backlog=1024
NoDelay=true

[Install]
WantedBy=sockets.target
EOF
    cat >"$service_file" <<EOF
$managed_marker
[Unit]
Description=Kodex raw TCP proxy from IPv6 port $port to IPv4 K3s ingress
Documentation=https://github.com/codex-k8s/kodex/blob/main/docs/runbooks/fresh-install.md
Requires=$unit_prefix-$port.socket
After=$unit_prefix-$port.socket network-online.target
Wants=network-online.target

[Service]
Type=notify
ExecStart=$proxy_binary --connections-max=$connection_limit --exit-idle-time=5min 127.0.0.1:$port
Restart=on-failure
RestartSec=1s
DynamicUser=yes
UMask=0077
NoNewPrivileges=yes
PrivateDevices=yes
PrivateTmp=yes
ProtectSystem=strict
ProtectHome=yes
ProtectHostname=yes
ProtectClock=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectKernelLogs=yes
ProtectControlGroups=yes
RestrictAddressFamilies=AF_INET AF_INET6
RestrictNamespaces=yes
RestrictRealtime=yes
RestrictSUIDSGID=yes
LockPersonality=yes
MemoryDenyWriteExecute=yes
CapabilityBoundingSet=
AmbientCapabilities=
SystemCallArchitectures=native
LimitNOFILE=4096
TasksMax=128
MemoryMax=256M
EOF
    chmod 0600 "$socket_file" "$service_file"
  done
}

assert_unit_files_exact() {
  local expected_directory=$1 port kind actual expected mode_value owner_uid
  for port in "${ports[@]}"; do
    for kind in socket service; do
      actual=$(unit_path "$port" "$kind")
      expected="$expected_directory/$unit_prefix-$port.$kind"
      [[ -f "$actual" && ! -L "$actual" ]] || fail "managed unit is absent: $actual"
      cmp -s "$expected" "$actual" || fail "managed unit content differs: $actual"
      mode_value=$(stat -c '%a' "$actual")
      [[ "$mode_value" == 644 ]] || fail "managed unit mode differs from 0644: $actual"
      if [[ -z "$test_root" ]]; then
        owner_uid=$(stat -c '%u' "$actual")
        [[ "$owner_uid" == 0 ]] || fail "managed unit owner is not root: $actual"
      fi
    done
  done
}

assert_socket_listening() {
  local port=$1 unit="$unit_prefix-$port.socket" substate
  systemctl is-enabled --quiet "$unit" || fail "socket unit is not enabled: $unit"
  systemctl is-active --quiet "$unit" || fail "socket unit is not active: $unit"
  substate=$(systemctl show --property=SubState --value "$unit") ||
    fail "socket unit state is unavailable: $unit"
  case "$substate" in
    listening|running) ;;
    *) fail "socket unit has an unexpected state: $unit ($substate)" ;;
  esac
  systemctl is-failed --quiet "$unit" && fail "socket unit failed: $unit"
  systemctl is-failed --quiet "$unit_prefix-$port.service" &&
    fail "proxy service failed: $unit_prefix-$port.service"
  return 0
}

probe_ipv6_http() {
  local address=$1 attempt status=""
  command -v curl >/dev/null 2>&1 || fail 'curl is required'
  for attempt in $(seq 1 60); do
    status=$(curl --noproxy '*' --ipv6 --silent --show-error --output /dev/null \
      --write-out '%{http_code}' --connect-timeout 1 --max-time 2 \
      --resolve "kodex-ipv6-ingress-probe.invalid:80:[$address]" \
      http://kodex-ipv6-ingress-probe.invalid/ 2>/dev/null || true)
    [[ "$status" =~ ^[1-5][0-9]{2}$ ]] && return 0
    sleep 2
  done
  fail 'local IPv6 HTTP request did not reach the K3s ingress'
}

assert_retired() {
  local port kind path unit
  for port in "${ports[@]}"; do
    for kind in socket service; do
      path=$(unit_path "$port" "$kind")
      [[ ! -e "$path" && ! -L "$path" ]] || fail "retired unit remains: $path"
      unit="$unit_prefix-$port.$kind"
      ! systemctl is-active --quiet "$unit" || fail "retired unit remains active: $unit"
      ! systemctl is-enabled --quiet "$unit" || fail "retired unit remains enabled: $unit"
    done
  done
}

retire_bridge() {
  local owner_units_present=false port kind path socket_path service_path
  assert_all_paths_safe
  for port in "${ports[@]}"; do
    for kind in socket service; do
      path=$(unit_path "$port" "$kind")
      [[ ! -e "$path" ]] || owner_units_present=true
    done
  done
  if [[ "$owner_units_present" == true ]]; then
    for port in "${ports[@]}"; do
      socket_path=$(unit_path "$port" socket)
      service_path=$(unit_path "$port" service)
      if [[ -e "$socket_path" ]]; then
        systemctl disable --now "$unit_prefix-$port.socket" >/dev/null 2>&1 || true
      fi
      if [[ -e "$service_path" ]]; then
        systemctl stop "$unit_prefix-$port.service" >/dev/null 2>&1 || true
      fi
      rm -f -- "$socket_path" "$service_path"
    done
    systemctl daemon-reload
    systemctl reset-failed \
      "$unit_prefix-80.service" "$unit_prefix-443.service" >/dev/null 2>&1 || true
  fi
  assert_retired
}

assert_all_paths_safe

if [[ -z "$server_public_ipv6_address" ]]; then
  case "$mode" in
    apply) retire_bridge ;;
    readback) assert_retired ;;
    preflight) ;;
  esac
  printf 'Kodex IPv6 ingress bridge completed: %s (disabled)\n' "$mode"
  exit 0
fi

normalized_ipv6_address=$(normalize_ipv6_address "$server_public_ipv6_address") ||
  fail 'server public IPv6 address is invalid or not globally routable'
assert_ipv6_address_on_host "$normalized_ipv6_address"
socket_proxy_binary=$(resolve_socket_proxy_binary)
temporary_directory=$(mktemp -d)
render_units "$normalized_ipv6_address" "$socket_proxy_binary" "$temporary_directory"

if [[ "$mode" == preflight ]]; then
  printf 'Kodex IPv6 ingress bridge completed: preflight\n'
  exit 0
fi

if [[ "$mode" == apply ]]; then
  install -d -m 0755 "$systemd_unit_directory"
  for port in "${ports[@]}"; do
    if [[ -n "$test_root" ]]; then
      install -m 0644 "$temporary_directory/$unit_prefix-$port.socket" \
        "$(unit_path "$port" socket)"
      install -m 0644 "$temporary_directory/$unit_prefix-$port.service" \
        "$(unit_path "$port" service)"
    else
      install -o root -g root -m 0644 "$temporary_directory/$unit_prefix-$port.socket" \
        "$(unit_path "$port" socket)"
      install -o root -g root -m 0644 "$temporary_directory/$unit_prefix-$port.service" \
        "$(unit_path "$port" service)"
    fi
  done
  systemctl daemon-reload
  systemctl enable \
    "$unit_prefix-80.socket" "$unit_prefix-443.socket" >/dev/null
  systemctl stop \
    "$unit_prefix-80.service" "$unit_prefix-443.service" >/dev/null 2>&1 || true
  systemctl restart \
    "$unit_prefix-80.socket" "$unit_prefix-443.socket" >/dev/null
fi

assert_unit_files_exact "$temporary_directory"
for port in "${ports[@]}"; do
  assert_socket_listening "$port"
done
probe_ipv6_http "$normalized_ipv6_address"
printf 'Kodex IPv6 ingress bridge completed: %s\n' "$mode"
