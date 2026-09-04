#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex host preparation failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 --mode preflight|apply|readback --server-public-ip <IPv4> --operator-user <name> [--server-public-ipv6-address <IPv6>]" >&2
}

mode=""
server_public_ip=""
server_public_ipv6_address=""
operator_user=""
while (($# > 0)); do
  case "$1" in
    --mode) mode="${2:-}"; shift 2 ;;
    --server-public-ip) server_public_ip="${2:-}"; shift 2 ;;
    --server-public-ipv6-address) server_public_ipv6_address="${2:-}"; shift 2 ;;
    --operator-user) operator_user="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

case "$mode" in preflight|apply|readback) ;; *) fail 'mode is invalid' ;; esac
[[ "$server_public_ip" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]] ||
  fail 'server public IPv4 is invalid'
[[ "$operator_user" =~ ^[a-z_][a-z0-9_-]{0,30}$ && "$operator_user" != root ]] ||
  fail 'unprivileged host operator user is required'
id "$operator_user" >/dev/null 2>&1 || fail 'host operator user is absent'
[[ "$(uname -s)" == Linux && "$(uname -m)" == x86_64 ]] ||
  fail 'only Linux x86_64 is supported by the bare-metal profile'
((EUID == 0)) || fail 'host preparation must run as root'

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
lock_file="$script_directory/components.lock.json"
ipv6_ingress_bridge_script="$script_directory/configure-ipv6-ingress-bridge.sh"
provider_apparmor_profile_source="$script_directory/../../infra/apparmor/kodex-provider-runtime"
provider_apparmor_profile_target=/etc/apparmor.d/kodex-provider-runtime
pod_cidr=10.42.0.0/16
host_service_address=10.254.254.1
k3s_resolver_file=/etc/rancher/k3s/resolv.conf
hot_reload_sysctl_file=/etc/sysctl.d/99-kodex-hot-reload.conf
local_api_interface=kodex-api0
local_api_service=kodex-local-api-address.service
sshd_drop_in=/etc/ssh/sshd_config.d/60-kodex-breakglass.conf
locked_host_packages=(containerd docker-buildx docker-compose-v2 docker.io runc)

validate_host_contract() {
  local expected_id expected_version expected_codename
  read -r expected_id expected_version expected_codename < <(python3 - "$lock_file" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as source:
    lock = json.load(source)
expected_packages = {"containerd", "docker-buildx", "docker-compose-v2", "docker.io", "runc"}
host = lock.get("host", {})
operating_system = host.get("os", {})
packages = host.get("packages", {})
if (lock.get("schemaVersion") != 1 or operating_system !=
        {"id": "ubuntu", "version": "24.04", "codename": "noble"} or
        set(packages) != expected_packages or
        any(not isinstance(value, str) or not value for value in packages.values())):
    raise SystemExit(1)
print(operating_system["id"], operating_system["version"], operating_system["codename"])
PY
  ) || fail 'host package lock is invalid'
  # shellcheck disable=SC1091
  source /etc/os-release
  [[ "${ID:-}" == "$expected_id" && "${VERSION_ID:-}" == "$expected_version" &&
    "${VERSION_CODENAME:-}" == "$expected_codename" ]] ||
    fail 'host operating system differs from the repository lock'
}

locked_host_package_version() {
  python3 - "$lock_file" "$1" <<'PY' || fail 'host package version is absent from the repository lock'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as source:
    value = json.load(source).get("host", {}).get("packages", {}).get(sys.argv[2])
if not isinstance(value, str) or not value:
    raise SystemExit(1)
print(value)
PY
}

readback_locked_host_packages() {
  local package expected actual
  for package in "${locked_host_packages[@]}"; do
    expected=$(locked_host_package_version "$package")
    actual=$(dpkg-query -W -f='${Version}' "$package" 2>/dev/null) ||
      fail 'locked host package is not installed'
    [[ "$actual" == "$expected" ]] || fail 'installed host package differs from the repository lock'
    apt-mark showhold | grep -Fxq "$package" || fail 'locked host package is not held'
  done
}

read_local_api_address() {
  local address_with_prefix address
  systemctl is-enabled --quiet "$local_api_service" 2>/dev/null || return 0
  systemctl is-active --quiet "$local_api_service" ||
    fail 'stable API address service is enabled but inactive'
  ip -d link show dev "$local_api_interface" |
    grep -Eq '(^|[[:space:]])dummy([[:space:]]|$)' ||
    fail 'stable API address interface is not a dummy interface'
  address_with_prefix=$(ip -4 -o address show dev "$local_api_interface" |
    awk 'NR == 1 {print $4} NR > 1 {exit 2}') ||
    fail 'stable API interface has multiple IPv4 addresses'
  [[ "$address_with_prefix" == */32 ]] ||
    fail 'stable API interface does not have one IPv4 /32 address'
  address=${address_with_prefix%/32}
  python3 - "$address" <<'PY' || fail 'stable API address is not a private non-loopback IPv4 address'
import ipaddress
import sys

address = ipaddress.ip_address(sys.argv[1])
if address.version != 4 or not address.is_private or address.is_loopback or address.is_unspecified:
    raise SystemExit(1)
PY
  printf '%s' "$address"
}

configure_provider_apparmor_profile() {
  [[ -f "$provider_apparmor_profile_source" && ! -L "$provider_apparmor_profile_source" ]] ||
    fail 'provider AppArmor profile source is absent'
  apparmor_parser -Q "$provider_apparmor_profile_source" >/dev/null ||
    fail 'provider AppArmor profile is invalid'
  install -m 0644 "$provider_apparmor_profile_source" "$provider_apparmor_profile_target"
  apparmor_parser -r "$provider_apparmor_profile_target"
}

readback_provider_apparmor_profile() {
  command -v apparmor_parser >/dev/null 2>&1 || fail 'apparmor_parser is required'
  [[ -f "$provider_apparmor_profile_target" && ! -L "$provider_apparmor_profile_target" ]] ||
    fail 'provider AppArmor profile is absent'
  cmp -s "$provider_apparmor_profile_source" "$provider_apparmor_profile_target" ||
    fail 'provider AppArmor profile differs from repository source'
  grep -Fxq 'kodex-provider-runtime (unconfined)' /sys/kernel/security/apparmor/profiles ||
    fail 'provider AppArmor profile is not loaded'
}

configure_k3s_resolver() {
  local source_file temporary_file nameserver_count
  source_file=/run/systemd/resolve/resolv.conf
  [[ -r "$source_file" ]] || source_file=/etc/resolv.conf
  temporary_file=$(mktemp)
  awk '
    $1 == "nameserver" &&
    $2 ~ /^([0-9]{1,3}\.){3}[0-9]{1,3}$/ &&
    $2 !~ /^127\./ &&
    !seen[$2]++ &&
    count < 3 {
      print "nameserver " $2
      count++
    }
  ' "$source_file" >"$temporary_file"
  nameserver_count=$(wc -l <"$temporary_file")
  [[ "$nameserver_count" -ge 1 ]] || {
    rm -f -- "$temporary_file"
    fail 'no non-loopback upstream DNS server is available for k3s'
  }
  {
    printf '%s\n' '# Managed by Kodex. Host search domains are intentionally excluded.'
    cat -- "$temporary_file"
    printf '%s\n' 'options timeout:2 attempts:2'
  } | install -m 0644 /dev/stdin "$k3s_resolver_file"
  rm -f -- "$temporary_file"
}

readback_k3s_resolver() {
  [[ -f "$k3s_resolver_file" && ! -L "$k3s_resolver_file" ]] ||
    fail 'dedicated k3s resolver file is absent'
  awk '
    $1 == "search" || $1 == "domain" { exit 1 }
    $1 == "nameserver" {
      if ($2 !~ /^([0-9]{1,3}\.){3}[0-9]{1,3}$/ || $2 ~ /^127\./ || seen[$2]++) exit 1
      count++
    }
    END { if (count < 1 || count > 3) exit 1 }
  ' "$k3s_resolver_file" || fail 'dedicated k3s resolver is unsafe'
  K3S_RESOLVER_FILE="$k3s_resolver_file" yq -e \
    '."resolv-conf" == strenv(K3S_RESOLVER_FILE)' /etc/rancher/k3s/config.yaml >/dev/null ||
    fail 'k3s does not use the dedicated resolver file'
}

remove_legacy_firewall() {
  systemctl disable --now nftables >/dev/null 2>&1 || true
  if command -v nft >/dev/null 2>&1 && nft list table inet kodex_fw >/dev/null 2>&1; then
    nft delete table inet kodex_fw
  fi
}

read_k3s_ipv4_nameservers() {
  awk '
    $1 == "nameserver" && $2 ~ /^([0-9]{1,3}\.){3}[0-9]{1,3}$/ && !seen[$2]++ {
      print $2
    }
  ' "$k3s_resolver_file"
}

require_k3s_ipv4_nameservers() {
  [[ "$(read_k3s_ipv4_nameservers | wc -l)" -ge 1 ]] ||
    fail 'no IPv4 upstream DNS server is available for the exact firewall policy'
}

read_default_ipv4_interface() {
  local route_output
  local -a interfaces=()
  route_output=$(ip -4 route show default) ||
    fail 'default IPv4 route could not be read'
  mapfile -t interfaces < <(awk '
    $1 == "default" {
      for (field_index = 1; field_index < NF; field_index++) {
        if ($field_index == "dev") print $(field_index + 1)
      }
    }
  ' <<<"$route_output" | sort -u)
  ((${#interfaces[@]} == 1)) ||
    fail 'exactly one default IPv4 interface is required'
  [[ "${interfaces[0]}" =~ ^[[:alnum:]_.:-]+$ ]] ||
    fail 'default IPv4 interface name is unsafe'
  printf '%s' "${interfaces[0]}"
}

normalize_ufw_rules() {
  python3 /dev/fd/3 3<<'PY'
import json
import re
import shlex
import sys

normalized = []
for raw_line in sys.stdin:
    line = raw_line.strip()
    if not line or line.startswith("Added user rules"):
        continue
    tokens = shlex.split(line)
    if not tokens or tokens[0] != "ufw":
        raise SystemExit(1)
    position = 1
    routed = position < len(tokens) and tokens[position] == "route"
    position += int(routed)
    if position >= len(tokens) or tokens[position] != "allow":
        raise SystemExit(1)
    position += 1
    if position < len(tokens) and re.fullmatch(r"[0-9]+/(tcp|udp)", tokens[position]):
        service = tokens[position]
        position += 1
        if position < len(tokens):
            if position + 2 != len(tokens) or tokens[position] != "comment":
                raise SystemExit(1)
            position += 2
        normalized.append({"route": routed, "service": service})
        continue
    rule = {
        "route": routed,
        "protocol": "any",
        "source": "any",
        "destination": "any",
        "port": "any",
        "inInterface": "",
        "outInterface": "",
    }
    seen = set()
    while position < len(tokens):
        token = tokens[position]
        if token == "comment":
            if position + 2 != len(tokens):
                raise SystemExit(1)
            position += 2
            continue
        if token in ("in", "out"):
            if token in seen or position + 2 >= len(tokens) or tokens[position + 1] != "on":
                raise SystemExit(1)
            rule[f"{token}Interface"] = tokens[position + 2]
            seen.add(token)
            position += 3
            continue
        field = {
            "proto": "protocol",
            "from": "source",
            "to": "destination",
            "port": "port",
        }.get(token)
        if field is None or field in seen or position + 1 >= len(tokens):
            raise SystemExit(1)
        rule[field] = tokens[position + 1]
        seen.add(field)
        position += 2
    normalized.append(rule)
for rule in sorted(normalized, key=lambda value: json.dumps(value, sort_keys=True)):
    print(json.dumps(rule, sort_keys=True, separators=(",", ":")))
PY
}

write_expected_firewall_rules() {
  local api_address nameserver public_interface
  require_k3s_ipv4_nameservers
  api_address=$(read_local_api_address)
  [[ -n "$api_address" ]] || api_address=$server_public_ip
  public_interface=$(read_default_ipv4_interface)
  printf '%s\n' \
    'ufw allow 22/tcp' \
    'ufw allow 80/tcp' \
    'ufw allow 443/tcp' \
    "ufw allow in on cni0 proto tcp from $pod_cidr to $api_address port 6443" \
    "ufw allow in on cni0 proto tcp from $pod_cidr to $server_public_ip port 10250" \
    "ufw allow in on cni0 proto tcp from $pod_cidr to $host_service_address port 3080" \
    "ufw allow in on cni0 proto tcp from $pod_cidr to $host_service_address port 18080" \
    "ufw route allow in on cni0 out on cni0 from $pod_cidr to $pod_cidr" \
    "ufw route allow in on cni0 out on flannel.1 from $pod_cidr to $pod_cidr" \
    "ufw route allow in on flannel.1 out on cni0 from $pod_cidr to $pod_cidr" \
    "ufw route allow in on cni0 out on $public_interface proto tcp from $pod_cidr to any port 80" \
    "ufw route allow in on cni0 out on $public_interface proto tcp from $pod_cidr to any port 443" \
    "ufw route allow in on flannel.1 out on $public_interface proto tcp from $pod_cidr to any port 80" \
    "ufw route allow in on flannel.1 out on $public_interface proto tcp from $pod_cidr to any port 443" \
    "ufw route allow proto tcp from any to $pod_cidr port 80" \
    "ufw route allow proto tcp from any to $pod_cidr port 443"
  while IFS= read -r nameserver; do
    printf '%s\n' \
      "ufw route allow in on cni0 proto udp from $pod_cidr to $nameserver port 53" \
      "ufw route allow in on cni0 proto tcp from $pod_cidr to $nameserver port 53"
  done < <(read_k3s_ipv4_nameservers)
}

configure_firewall() {
  local api_address nameserver public_interface
  remove_legacy_firewall
  require_k3s_ipv4_nameservers
  api_address=$(read_local_api_address)
  [[ -n "$api_address" ]] || api_address=$server_public_ip
  public_interface=$(read_default_ipv4_interface)
  ufw --force reset >/dev/null
  ufw default deny incoming >/dev/null
  ufw default allow outgoing >/dev/null
  ufw default deny routed >/dev/null
  ufw allow 22/tcp comment SSH >/dev/null
  ufw allow 80/tcp comment 'HTTP ingress' >/dev/null
  ufw allow 443/tcp comment 'HTTPS ingress' >/dev/null
  ufw allow in on cni0 proto tcp from "$pod_cidr" to "$api_address" port 6443 \
    comment 'K3s API from pods' >/dev/null
  ufw allow in on cni0 proto tcp from "$pod_cidr" to "$server_public_ip" port 10250 \
    comment 'Kubelet metrics from pods' >/dev/null
  ufw allow in on cni0 proto tcp from "$pod_cidr" to "$host_service_address" port 3080 \
    comment 'Teleport backend from pods' >/dev/null
  ufw allow in on cni0 proto tcp from "$pod_cidr" to "$host_service_address" port 18080 \
    comment 'ACME preflight responder from pods' >/dev/null
  ufw route allow in on cni0 out on cni0 from "$pod_cidr" to "$pod_cidr" \
    comment 'K3s same-node pod overlay' >/dev/null
  ufw route allow in on cni0 out on flannel.1 from "$pod_cidr" to "$pod_cidr" \
    comment 'K3s pod overlay egress' >/dev/null
  ufw route allow in on flannel.1 out on cni0 from "$pod_cidr" to "$pod_cidr" \
    comment 'K3s pod overlay ingress' >/dev/null
  ufw route allow in on cni0 out on "$public_interface" proto tcp \
    from "$pod_cidr" to any port 80 comment 'K3s pod HTTP egress' >/dev/null
  ufw route allow in on cni0 out on "$public_interface" proto tcp \
    from "$pod_cidr" to any port 443 comment 'K3s pod HTTPS egress' >/dev/null
  ufw route allow in on flannel.1 out on "$public_interface" proto tcp \
    from "$pod_cidr" to any port 80 comment 'K3s remote pod HTTP egress' >/dev/null
  ufw route allow in on flannel.1 out on "$public_interface" proto tcp \
    from "$pod_cidr" to any port 443 comment 'K3s remote pod HTTPS egress' >/dev/null
  ufw route allow proto tcp from any to "$pod_cidr" port 80 \
    comment 'Traefik HTTP ingress DNAT' >/dev/null
  ufw route allow proto tcp from any to "$pod_cidr" port 443 \
    comment 'Traefik HTTPS ingress DNAT' >/dev/null
  while IFS= read -r nameserver; do
    ufw route allow in on cni0 proto udp from "$pod_cidr" to "$nameserver" port 53 \
      comment 'K3s upstream DNS UDP' >/dev/null
    ufw route allow in on cni0 proto tcp from "$pod_cidr" to "$nameserver" port 53 \
      comment 'K3s upstream DNS TCP' >/dev/null
  done < <(read_k3s_ipv4_nameservers)
  ufw --force enable >/dev/null
}

configure_sshd() {
  local temporary_file
  temporary_file=$(mktemp)
  cat >"$temporary_file" <<EOF
# Managed by Kodex. Public break-glass SSH is key-only and owner-scoped.
PubkeyAuthentication yes
PasswordAuthentication no
KbdInteractiveAuthentication no
ChallengeResponseAuthentication no
PermitRootLogin no
AuthenticationMethods publickey
AllowUsers $operator_user
EOF
  install -d -m 0755 /etc/ssh/sshd_config.d
  install -m 0644 -o root -g root "$temporary_file" "$sshd_drop_in"
  rm -f -- "$temporary_file"
  /usr/sbin/sshd -t || fail 'managed SSH configuration is invalid'
  systemctl reload ssh
}

readback_sshd() {
  local effective
  [[ -f "$sshd_drop_in" && ! -L "$sshd_drop_in" &&
    "$(stat -c '%u:%g:%a' "$sshd_drop_in")" == 0:0:644 ]] ||
    fail 'managed SSH configuration is absent or unsafe'
  systemctl is-active --quiet ssh || fail 'SSH service is not active'
  /usr/sbin/sshd -t || fail 'SSH configuration readback failed'
  effective=$(/usr/sbin/sshd -T -C "user=$operator_user,host=localhost,addr=127.0.0.1") ||
    fail 'effective SSH configuration readback failed'
  grep -Fxq 'pubkeyauthentication yes' <<<"$effective" || fail 'SSH public-key authentication is disabled'
  grep -Fxq 'passwordauthentication no' <<<"$effective" || fail 'SSH password authentication is enabled'
  grep -Fxq 'kbdinteractiveauthentication no' <<<"$effective" || fail 'SSH keyboard-interactive authentication is enabled'
  grep -Fxq 'permitrootlogin no' <<<"$effective" || fail 'SSH root login is enabled'
  grep -Fxq 'authenticationmethods publickey' <<<"$effective" || fail 'SSH authentication methods are not key-only'
  [[ "$(awk '$1 == "allowusers" { print; count++ } END { if (count != 1) exit 1 }' <<<"$effective")" == "allowusers $operator_user" ]] ||
    fail 'SSH allowed users differ from the exact operator identity'
}

readback_firewall() {
  local status expected_rules actual_rules
  command -v nft >/dev/null 2>&1 && nft list table inet kodex_fw >/dev/null 2>&1 &&
    fail 'legacy kodex_fw nftables policy is active'
  systemctl is-enabled --quiet nftables && fail 'nftables autoload remains enabled'
  status=$(ufw status verbose)
  grep -Fq 'Status: active' <<<"$status" || fail 'host firewall is inactive'
  grep -Fq 'Default: deny (incoming), allow (outgoing), deny (routed)' <<<"$status" ||
    fail 'host firewall defaults differ from the supported policy'
  expected_rules=$(write_expected_firewall_rules | normalize_ufw_rules) ||
    fail 'expected firewall policy could not be normalized'
  actual_rules=$(ufw show added | normalize_ufw_rules) ||
    fail 'active firewall policy could not be normalized'
  [[ "$actual_rules" == "$expected_rules" ]] ||
    fail 'host firewall rules differ from the exact supported policy'
}

readback_k3s_forwarding() {
  local forward_rules kube_router_line policy_accept_line ufw_line link_details
  [[ "$(sysctl -n net.ipv4.ip_forward)" == 1 ]] ||
    fail 'IPv4 forwarding is disabled'
  link_details=$(ip -d link show dev cni0) || fail 'cni0 interface is absent'
  grep -Eq '<([^,>]*,)*UP(,[^>]*)*>' <<<"$link_details" ||
    fail 'cni0 interface is down'
  grep -Eq '(^|[[:space:]])bridge([[:space:]]|$)' <<<"$link_details" ||
    fail 'cni0 interface is not a bridge'
  link_details=$(ip -d link show dev flannel.1) || fail 'flannel.1 interface is absent'
  grep -Eq '<([^,>]*,)*UP(,[^>]*)*>' <<<"$link_details" ||
    fail 'flannel.1 interface is down'
  grep -Eq 'vxlan id 1([^0-9]|$).*dstport 8472([^0-9]|$)' <<<"$link_details" ||
    fail 'flannel.1 VXLAN settings differ from the supported profile'
  forward_rules=$(iptables -w 5 -S FORWARD) ||
    fail 'iptables FORWARD chain could not be read'
  kube_router_line=$(awk '
    $1 == "-A" && $2 == "FORWARD" && $NF == "KUBE-ROUTER-FORWARD" { count++; line=NR }
    END { if (count == 1) print line; else exit 1 }
  ' <<<"$forward_rules") || fail 'KUBE-ROUTER-FORWARD hook is not exact'
  policy_accept_line=$(awk '
    $1 == "-A" && $2 == "FORWARD" && $NF == "ACCEPT" {
      for (field_index = 1; field_index < NF; field_index++) {
        if ($field_index == "--mark" && $(field_index + 1) == "0x20000/0x20000") {
          count++; line=NR
        }
      }
    }
    END { if (count == 1) print line; else exit 1 }
  ' <<<"$forward_rules") || fail 'Kubernetes NetworkPolicy accept hook is not exact'
  ufw_line=$(awk '
    $1 == "-A" && $2 == "FORWARD" && $NF == "ufw-before-forward" { count++; line=NR }
    END { if (count == 1) print line; else exit 1 }
  ' <<<"$forward_rules") || fail 'UFW FORWARD hook is not exact'
  ((kube_router_line < policy_accept_line && policy_accept_line < ufw_line)) ||
    fail 'Kubernetes NetworkPolicy hooks do not precede UFW forwarding'
}

configure_hot_reload_sysctl() {
  cat <<'EOF' | install -m 0644 /dev/stdin "$hot_reload_sysctl_file"
# Управляется Kodex для host-owned hot reload контура.
fs.inotify.max_user_instances = 1024
fs.inotify.max_user_watches = 524288
EOF
  sysctl --load "$hot_reload_sysctl_file" >/dev/null ||
    fail 'hot reload sysctl settings could not be applied'
}

readback_hot_reload_sysctl() {
  local expected_file
  [[ -f "$hot_reload_sysctl_file" && ! -L "$hot_reload_sysctl_file" ]] ||
    fail 'hot reload sysctl file is absent or unsafe'
  expected_file=$(mktemp)
  cat >"$expected_file" <<'EOF'
# Управляется Kodex для host-owned hot reload контура.
fs.inotify.max_user_instances = 1024
fs.inotify.max_user_watches = 524288
EOF
  cmp -s "$expected_file" "$hot_reload_sysctl_file" || {
    rm -f -- "$expected_file"
    fail 'hot reload sysctl file differs from the repository contract'
  }
  rm -f -- "$expected_file"
  [[ "$(sysctl -n fs.inotify.max_user_instances)" == 1024 ]] ||
    fail 'fs.inotify.max_user_instances differs from the supported value'
  [[ "$(sysctl -n fs.inotify.max_user_watches)" == 524288 ]] ||
    fail 'fs.inotify.max_user_watches differs from the supported value'
}

validate_host_contract
if [[ "$mode" == apply ]]; then
  locked_package_arguments=()
  for package in "${locked_host_packages[@]}"; do
    locked_package_arguments+=("$package=$(locked_host_package_version "$package")")
  done
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq
  apt-get install -y -qq --allow-downgrades --allow-change-held-packages \
    apache2-utils apparmor apparmor-utils build-essential ca-certificates curl dnsutils gh git iptables jq \
    libnss3-tools make iproute2 openssl python3 ripgrep rsync systemd tar unzip \
    openssh-server uidmap ufw xz-utils zstd "${locked_package_arguments[@]}"
  apt-mark hold "${locked_host_packages[@]}" >/dev/null
else
  command -v python3 >/dev/null 2>&1 || fail 'python3 is required'
fi
python3 - "$lock_file" <<'PY' || fail 'component lock is invalid'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as source:
    lock = json.load(source)
if lock.get("schemaVersion") != 1:
    raise SystemExit(1)
artifacts = lock.get("artifacts", [])
if len(artifacts) != 10 or len(lock.get("charts", [])) != 1:
    raise SystemExit(1)
for artifact in artifacts:
    sha256 = artifact.get("sha256", "")
    integrity = artifact.get("integrity", "")
    if bool(sha256) == bool(integrity):
        raise SystemExit(1)
    if sha256 and (len(sha256) != 64 or any(char not in "0123456789abcdef" for char in sha256)):
        raise SystemExit(1)
    if integrity and not integrity.startswith("sha512-"):
        raise SystemExit(1)
PY

"$ipv6_ingress_bridge_script" --mode preflight \
  --server-public-ipv6-address "$server_public_ipv6_address"

if [[ "$mode" == preflight ]]; then
  command -v curl >/dev/null 2>&1 || fail 'curl is required'
  [[ -d /run/systemd/system ]] || fail 'systemd is required'
  [[ -w /usr/local/bin ]] || fail '/usr/local/bin is not writable'
  printf 'Kodex host preflight completed\n'
  exit 0
fi

download_artifact() {
  local name=$1 output=$2 url expected_sha actual_sha
  url=$(jq -er --arg name "$name" '.artifacts[] | select(.name == $name) | .url' "$lock_file")
  expected_sha=$(jq -er --arg name "$name" '.artifacts[] | select(.name == $name) | .sha256' "$lock_file")
  curl --proto '=https' --tlsv1.2 --fail --silent --show-error --location \
    --retry 5 --retry-all-errors --retry-delay 2 --connect-timeout 15 \
    "$url" --output "$output"
  actual_sha=$(sha256sum "$output" | awk '{print $1}')
  [[ "$actual_sha" == "$expected_sha" ]] || fail "artifact digest mismatch: $name"
}

download_integrity_artifact() {
  local name=$1 output=$2 url expected_integrity actual_integrity
  url=$(jq -er --arg name "$name" '.artifacts[] | select(.name == $name) | .url' "$lock_file")
  expected_integrity=$(jq -er --arg name "$name" \
    '.artifacts[] | select(.name == $name) | .integrity' "$lock_file")
  curl --proto '=https' --tlsv1.2 --fail --silent --show-error --location \
    --retry 5 --retry-all-errors --retry-delay 2 --connect-timeout 15 \
    "$url" --output "$output"
  actual_integrity="sha512-$(openssl dgst -sha512 -binary "$output" | base64 -w0)"
  [[ "$actual_integrity" == "$expected_integrity" ]] ||
    fail "artifact integrity mismatch: $name"
}

extract_npm_package() {
  local archive=$1 target=$2
  tar -tzf "$archive" | awk '
    !/^package\// || /(^|\/)\.\.(\/|$)/ || /^\// { exit 1 }
  ' || fail 'npm package archive contains an unsafe path'
  install -d -m 0755 "$target"
  tar -xzf "$archive" -C "$target" --strip-components=1 \
    --no-same-owner --no-same-permissions
}

if [[ "$mode" == apply ]]; then
  temporary_directory=$(mktemp -d)
  trap 'rm -rf -- "$temporary_directory"' EXIT
  download_artifact go "$temporary_directory/go.tar.gz"
  rm -rf -- /usr/local/go
  tar -xzf "$temporary_directory/go.tar.gz" -C /usr/local
  ln -sfn /usr/local/go/bin/go /usr/local/bin/go
  ln -sfn /usr/local/go/bin/gofmt /usr/local/bin/gofmt

  download_artifact node "$temporary_directory/node.tar.xz"
  rm -rf -- /usr/local/node
  mkdir -p /usr/local/node
  tar -xJf "$temporary_directory/node.tar.xz" -C /usr/local/node --strip-components=1
  for node_command in node npm npx corepack; do
    ln -sfn "/usr/local/node/bin/$node_command" "/usr/local/bin/$node_command"
  done
  # Сетевой npm install '@openai/codex@0.152.0' намеренно запрещён: пакеты
  # извлекаются только после проверки repo-owned SHA-512 без lifecycle scripts.
  codex_install_root=/usr/local/lib/kodex-cli
  download_integrity_artifact codex-cli "$temporary_directory/codex-cli.tgz"
  download_integrity_artifact codex-linux-x64 "$temporary_directory/codex-linux-x64.tgz"
  rm -rf -- "$codex_install_root"
  extract_npm_package "$temporary_directory/codex-cli.tgz" \
    "$codex_install_root/node_modules/@openai/codex"
  extract_npm_package "$temporary_directory/codex-linux-x64.tgz" \
    "$codex_install_root/node_modules/@openai/codex-linux-x64"
  chown -R root:root "$codex_install_root"
  chmod -R go-w "$codex_install_root"
  chmod 0755 "$codex_install_root/node_modules/@openai/codex/bin/codex.js"
  ln -sfn "$codex_install_root/node_modules/@openai/codex/bin/codex.js" \
    /usr/local/bin/codex

  configure_provider_apparmor_profile

  download_artifact helm "$temporary_directory/helm.tar.gz"
  tar -xzf "$temporary_directory/helm.tar.gz" -C "$temporary_directory"
  install -m 0755 "$temporary_directory/linux-amd64/helm" /usr/local/bin/helm
  download_artifact yq "$temporary_directory/yq"
  install -m 0755 "$temporary_directory/yq" /usr/local/bin/yq
  download_artifact cosign "$temporary_directory/cosign"
  install -m 0755 "$temporary_directory/cosign" /usr/local/bin/cosign
  download_artifact nsc "$temporary_directory/nsc.zip"
  unzip -q "$temporary_directory/nsc.zip" -d "$temporary_directory/nsc"
  nsc_binary=$(find "$temporary_directory/nsc" -type f -name nsc -print -quit)
  [[ -n "$nsc_binary" ]] || fail 'nsc binary is absent from the archive'
  install -m 0755 "$nsc_binary" /usr/local/bin/nsc

  download_artifact teleport-client "$temporary_directory/teleport-client.tar.gz"
  tar -xzf "$temporary_directory/teleport-client.tar.gz" -C "$temporary_directory" \
    teleport/teleport teleport/tctl teleport/tsh
  install -m 0755 "$temporary_directory/teleport/teleport" /usr/local/bin/teleport
  install -m 0755 "$temporary_directory/teleport/tctl" /usr/local/bin/tctl
  install -m 0755 "$temporary_directory/teleport/tsh" /usr/local/bin/tsh

  download_artifact k3s "$temporary_directory/k3s"
  install -m 0755 "$temporary_directory/k3s" /usr/local/bin/k3s
  rm -f -- /usr/local/bin/kubectl
  cat >/usr/local/bin/kubectl <<'EOF'
#!/bin/sh
export K3S_CONFIG_FILE=/dev/null
exec /usr/local/bin/k3s kubectl "$@"
EOF
  chmod 0755 /usr/local/bin/kubectl
  ln -sfn /usr/local/bin/k3s /usr/local/bin/crictl
  mkdir -p /etc/rancher/k3s /var/lib/rancher/k3s
  configure_k3s_resolver
  local_api_address=$(read_local_api_address)
  SERVER_PUBLIC_IP="$server_public_ip" K3S_RESOLVER_FILE="$k3s_resolver_file" \
    yq -n -o=yaml '
      ."write-kubeconfig-mode" = "0600" |
      ."secrets-encryption" = true |
      ."resolv-conf" = strenv(K3S_RESOLVER_FILE) |
      .disable = ["traefik"] |
      ."tls-san" = [strenv(SERVER_PUBLIC_IP)] |
      ."kubelet-arg" = ["max-pods=250"]
    ' >/etc/rancher/k3s/config.yaml
  if [[ -n "$local_api_address" ]]; then
    LOCAL_API_ADDRESS="$local_api_address" yq -i '
      ."advertise-address" = strenv(LOCAL_API_ADDRESS) |
      ."tls-san" += [strenv(LOCAL_API_ADDRESS)]
    ' /etc/rancher/k3s/config.yaml
  fi
  chmod 0600 /etc/rancher/k3s/config.yaml
  cat >/etc/systemd/system/k3s.service <<'EOF'
[Unit]
Description=Lightweight Kubernetes
Documentation=https://docs.k3s.io/
Wants=network-online.target
After=network-online.target

[Service]
Type=notify
EnvironmentFile=-/etc/default/k3s
KillMode=process
Delegate=yes
LimitNOFILE=1048576
LimitNPROC=infinity
LimitCORE=infinity
TasksMax=infinity
TimeoutStartSec=0
Restart=always
RestartSec=5s
ExecStart=/usr/local/bin/k3s server --config /etc/rancher/k3s/config.yaml

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  configure_hot_reload_sysctl
  configure_sshd
  configure_firewall
  systemctl enable --now docker >/dev/null
  usermod -aG docker "$operator_user"
  systemctl enable k3s >/dev/null
  systemctl restart k3s
fi

systemctl is-active --quiet k3s || fail 'k3s service is not active'
for command_name in certutil codex cosign dig docker go helm node npm kubectl nsc tsh yq; do
  command -v "$command_name" >/dev/null 2>&1 || fail "installed command is absent: $command_name"
done
for command_name in tctl teleport; do
  command -v "$command_name" >/dev/null 2>&1 || fail "installed command is absent: $command_name"
done
locked_artifact_version() {
  jq -er --arg name "$1" '.artifacts[] | select(.name == $name) | .version' "$lock_file"
}
require_locked_version() {
  local name=$1 actual=$2 expected
  expected=$(locked_artifact_version "$name")
  [[ "${actual#v}" == "${expected#v}" ]] ||
    fail "installed $name version differs from the component lock"
}
require_locked_version go "$(/usr/local/bin/go version | awk 'NR == 1 {sub(/^go/, "", $3); print $3}')"
require_locked_version node "$(/usr/local/bin/node --version)"
require_locked_version helm "$(/usr/local/bin/helm version --short | sed 's/+.*$//')"
require_locked_version yq "$(/usr/local/bin/yq --version | awk 'NR == 1 {print $NF}')"
require_locked_version cosign "$(/usr/local/bin/cosign version --json | jq -er .gitVersion)"
require_locked_version nsc "$(/usr/local/bin/nsc --version | awk 'NR == 1 {print $NF}')"
require_locked_version k3s "$(K3S_CONFIG_FILE=/dev/null /usr/local/bin/k3s --version | awk 'NR == 1 {print $3}')"
teleport_client_version=$(jq -er '.artifacts[] | select(.name == "teleport-client") | .version' "$lock_file")
[[ "$(tsh version --format=json | jq -r .version)" == "$teleport_client_version" ]] ||
  fail 'installed Teleport client version differs from the component lock'
for teleport_command in teleport tctl; do
  teleport_version=$(/usr/local/bin/"$teleport_command" version | awk '
    NR == 1 && $1 == "Teleport" { sub(/^v/, "", $2); print $2 }
  ')
  [[ "$teleport_version" == "$teleport_client_version" ]] ||
    fail "installed $teleport_command version differs from the component lock"
done
codex_version=$(runuser --user nobody -- env HOME=/tmp /usr/local/bin/codex --version |
  awk 'NR == 1 && $1 == "codex-cli" {print $2}')
require_locked_version codex-cli "$codex_version"
codex_package_root=/usr/local/lib/kodex-cli/node_modules/@openai
jq -e --arg version "$(locked_artifact_version codex-cli)" '.version == $version' \
  "$codex_package_root/codex/package.json" >/dev/null ||
  fail 'installed Codex CLI package version differs from the component lock'
jq -e --arg version "$(locked_artifact_version codex-linux-x64)" '.version == $version' \
  "$codex_package_root/codex-linux-x64/package.json" >/dev/null ||
  fail 'installed Codex platform package version differs from the component lock'
systemctl is-active --quiet docker || fail 'Docker service is not active'
docker buildx version >/dev/null 2>&1 || fail 'Docker buildx is unavailable'
readback_locked_host_packages
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
api_ready=false
node_ready=false
for _ in $(seq 1 120); do
  if kubectl get --raw=/readyz >/dev/null 2>&1; then
    api_ready=true
    if [[ "$(kubectl get node -o json 2>/dev/null | jq '[.items[] | select(any(.status.conditions[]?; .type == "Ready" and .status == "True"))] | length')" -ge 1 ]]; then
      node_ready=true
      break
    fi
  fi
  sleep 2
done
[[ "$api_ready" == true ]] || fail 'Kubernetes API did not become ready'
[[ "$node_ready" == true ]] || fail 'no ready Kubernetes node became available'
readback_k3s_resolver
readback_k3s_forwarding
readback_sshd
readback_firewall
readback_hot_reload_sysctl
readback_provider_apparmor_profile
if [[ "$mode" == apply ]]; then
  "$ipv6_ingress_bridge_script" --mode apply \
    --server-public-ipv6-address "$server_public_ipv6_address"
else
  "$ipv6_ingress_bridge_script" --mode readback \
    --server-public-ipv6-address "$server_public_ipv6_address"
fi
printf 'Kodex host preparation completed: %s\n' "$mode"
