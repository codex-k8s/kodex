#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex host hardening contract test failed: %s\n' "$*" >&2
  exit 1
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
prepare_host="$repository_root/tools/install/prepare-host.sh"
bootstrap_cluster="$repository_root/tools/dev/bootstrap-cluster.sh"
lock_file="$repository_root/tools/install/components.lock.json"
temporary_directory=$(mktemp -d)
cleanup() { rm -rf -- "$temporary_directory"; }
trap cleanup EXIT

bash -n "$prepare_host" "$bootstrap_cluster"

jq -e '
  .schemaVersion == 1 and
  .host.os == {id:"ubuntu",version:"24.04",codename:"noble"} and
  (.host.packages | keys | sort) ==
    (["containerd","docker-buildx","docker-compose-v2","docker.io","runc"] | sort) and
  all(.host.packages[]; type == "string" and length > 0) and
  (.artifacts | length) == 10 and (.charts | length) == 1 and
  ([.artifacts[].name] | unique | length) == 10 and
  all(.artifacts[];
    ((has("sha256") and (.sha256 | test("^[a-f0-9]{64}$"))) or
     (has("integrity") and (.integrity | test("^sha512-[A-Za-z0-9+/]+={0,2}$")))) and
    ((has("sha256") and (has("integrity") | not)) or
     (has("integrity") and (has("sha256") | not)))) and
  any(.artifacts[];
    .name == "codex-cli" and .version == "0.152.0" and
    (.url | endswith("/codex-0.152.0.tgz")) and has("integrity")) and
  any(.artifacts[];
    .name == "codex-linux-x64" and .version == "0.152.0-linux-x64" and
    (.url | endswith("/codex-0.152.0-linux-x64.tgz")) and has("integrity"))
' "$lock_file" >/dev/null || fail 'component integrity lock is invalid'

if rg -n 'apt-get[[:space:]]+upgrade' "$prepare_host" >/dev/null; then
  fail 'unbounded host package upgrade remains'
fi
for package_contract in \
  'validate_host_contract' \
  'locked_host_package_version' \
  'apt-get install -y -qq --allow-downgrades --allow-change-held-packages' \
  'apt-mark hold "${locked_host_packages[@]}"' \
  'readback_locked_host_packages'; do
  rg -Fq -- "$package_contract" "$prepare_host" ||
    fail "host package lock contract is absent: $package_contract"
done

for forbidden_firewall_rule in \
  'ufw allow from "$pod_cidr"' \
  'ufw allow from "$service_cidr"' \
  'ufw route allow from "$pod_cidr"' \
  'ufw allow 3080/tcp' \
  'ufw allow 18080/tcp' \
  'ufw allow proto tcp from any to "$host_service_address" port 3080' \
  'ufw allow proto tcp from any to "$host_service_address" port 18080'; do
  if rg -Fq "$forbidden_firewall_rule" "$prepare_host"; then
    fail "blanket firewall rule remains: $forbidden_firewall_rule"
  fi
done

for exact_firewall_contract in \
  'host_service_address=10.254.254.1' \
  'ufw allow in on cni0 proto tcp from "$pod_cidr" to "$api_address" port 6443' \
  'ufw allow in on cni0 proto tcp from "$pod_cidr" to "$server_public_ip" port 10250' \
  'ufw allow in on cni0 proto tcp from "$pod_cidr" to "$host_service_address" port 3080' \
  'ufw allow in on cni0 proto tcp from "$pod_cidr" to "$host_service_address" port 18080' \
  'ufw route allow in on cni0 out on cni0 from "$pod_cidr" to "$pod_cidr"' \
  'ufw route allow in on cni0 out on flannel.1 from "$pod_cidr" to "$pod_cidr"' \
  'ufw route allow in on flannel.1 out on cni0 from "$pod_cidr" to "$pod_cidr"' \
  '"ufw route allow in on cni0 out on $public_interface proto tcp from $pod_cidr to any port 80"' \
  '"ufw route allow in on cni0 out on $public_interface proto tcp from $pod_cidr to any port 443"' \
  '"ufw route allow in on flannel.1 out on $public_interface proto tcp from $pod_cidr to any port 80"' \
  '"ufw route allow in on flannel.1 out on $public_interface proto tcp from $pod_cidr to any port 443"' \
  'ufw route allow proto tcp from any to "$pod_cidr" port 80' \
  'ufw route allow proto tcp from any to "$pod_cidr" port 443' \
  'ufw route allow in on cni0 proto udp from "$pod_cidr" to "$nameserver" port 53' \
  'ufw route allow in on cni0 proto tcp from "$pod_cidr" to "$nameserver" port 53' \
  'normalize_ufw_rules' \
  'actual_rules=$(ufw show added' \
  '[[ "$actual_rules" == "$expected_rules" ]]' \
  'readback_k3s_forwarding' \
  'Kubernetes NetworkPolicy hooks do not precede UFW forwarding'; do
  rg -Fq "$exact_firewall_contract" "$prepare_host" ||
    fail "exact firewall contract is absent: $exact_firewall_contract"
done

for sysctl_contract in \
  'hot_reload_sysctl_file=/etc/sysctl.d/99-kodex-hot-reload.conf' \
  'fs.inotify.max_user_instances = 1024' \
  'fs.inotify.max_user_watches = 524288' \
  'sysctl --load "$hot_reload_sysctl_file"' \
  'readback_hot_reload_sysctl'; do
  rg -Fq "$sysctl_contract" "$prepare_host" ||
    fail "host-owned sysctl contract is absent: $sysctl_contract"
done

for ssh_contract in \
  'sshd_drop_in=/etc/ssh/sshd_config.d/60-kodex-breakglass.conf' \
  'PasswordAuthentication no' \
  'KbdInteractiveAuthentication no' \
  'PermitRootLogin no' \
  'AuthenticationMethods publickey' \
  'AllowUsers $operator_user' \
  '/usr/sbin/sshd -T -C "user=$operator_user,host=localhost,addr=127.0.0.1"' \
  '[[ "$(awk '\''$1 == "allowusers" { print; count++ } END { if (count != 1) exit 1 }'\'' <<<"$effective")" == "allowusers $operator_user" ]]' \
  'usermod -aG docker "$operator_user"'; do
  rg -Fq -- "$ssh_contract" "$prepare_host" ||
    fail "host SSH boundary is absent: $ssh_contract"
done
for forbidden_tuning_contract in \
  'kodex-local-host-tuning' \
  '/host-proc-sys-fs-inotify' \
  'apply_hot_reload_host_tuning' \
  'readback_hot_reload_host_tuning'; do
  if rg -Fq "$forbidden_tuning_contract" "$bootstrap_cluster"; then
    fail "privileged host-tuning workload remains: $forbidden_tuning_contract"
  fi
done

if rg -n '^[[:space:]]*npm[[:space:]]+install' "$prepare_host" >/dev/null; then
  fail 'Codex CLI is still installed through npm execution'
fi
for codex_contract in \
  'download_integrity_artifact codex-cli' \
  'download_integrity_artifact codex-linux-x64' \
  'openssl dgst -sha512 -binary' \
  'extract_npm_package' \
  '--no-same-owner --no-same-permissions' \
  'runuser --user nobody -- env HOME=/tmp /usr/local/bin/codex --version' \
  'require_locked_version codex-cli'; do
  rg -Fq -- "$codex_contract" "$prepare_host" ||
    fail "Codex integrity contract is absent: $codex_contract"
done

for teleport_binary in teleport tctl tsh; do
  rg -Fq "teleport/$teleport_binary" "$prepare_host" ||
    fail "Teleport archive member is not installed: $teleport_binary"
done
for version_contract in \
  'require_locked_version go' \
  'require_locked_version node' \
  'require_locked_version helm' \
  'require_locked_version yq' \
  'require_locked_version cosign' \
  'require_locked_version nsc' \
  'require_locked_version k3s' \
  'installed $teleport_command version differs from the component lock'; do
  rg -Fq "$version_contract" "$prepare_host" ||
    fail "exact executable version readback is absent: $version_contract"
done

source <(sed -n '/^read_k3s_ipv4_nameservers() {$/,/^}$/p' "$prepare_host")
source <(sed -n '/^require_k3s_ipv4_nameservers() {$/,/^}$/p' "$prepare_host")
source <(sed -n '/^read_default_ipv4_interface() {$/,/^}$/p' "$prepare_host")
source <(sed -n '/^normalize_ufw_rules() {$/,/^}$/p' "$prepare_host")
source <(sed -n '/^write_expected_firewall_rules() {$/,/^}$/p' "$prepare_host")
source <(sed -n '/^readback_firewall() {$/,/^}$/p' "$prepare_host")
source <(sed -n '/^readback_k3s_forwarding() {$/,/^}$/p' "$prepare_host")
normalized_declared=$(printf '%s\n' \
  'ufw route allow proto tcp from any to 10.42.0.0/16 port 80' | normalize_ufw_rules)
normalized_reported=$(printf '%s\n' \
  'ufw route allow to 10.42.0.0/16 port 80 proto tcp from any' | normalize_ufw_rules)
[[ "$normalized_declared" == "$normalized_reported" ]] ||
  fail 'firewall normalization depends on UFW token order'
if printf '%s\n' 'ufw route allow unexpected token' | normalize_ufw_rules >/dev/null 2>&1; then
  fail 'firewall normalization accepted unknown tokens'
fi
normalized_service=$(printf '%s\n' "ufw allow 22/tcp comment 'SSH'" | normalize_ufw_rules)
[[ "$normalized_service" == '{"route":false,"service":"22/tcp"}' ]] ||
  fail 'firewall normalization rejected a commented service rule'
pod_cidr=10.42.0.0/16
server_public_ip=203.0.113.10
host_service_address=10.254.254.1
k3s_resolver_file="$temporary_directory/resolv.conf"
printf 'nameserver 1.1.1.1\n' >"$k3s_resolver_file"
read_local_api_address() { printf '10.0.0.1'; }
ip() {
  case "$*" in
    '-4 route show default') printf '%s\n' 'default via 203.0.113.1 dev eth0 proto dhcp' ;;
    '-d link show dev cni0') printf '%s\n' '2: cni0: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1450 state UP' '    bridge forward_delay 0' ;;
    '-d link show dev flannel.1') printf '%s\n' '3: flannel.1: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1450 state UNKNOWN' '    vxlan id 1 local 203.0.113.10 dev eth0 srcport 0 0 dstport 8472 nolearning' ;;
    *) return 1 ;;
  esac
}
systemctl() { return 1; }
nft() { return 1; }
expected_firewall_rules=$(write_expected_firewall_rules)
ufw() {
  if [[ "$*" == 'status verbose' ]]; then
    printf '%s\n' \
      'Status: active' \
      'Default: deny (incoming), allow (outgoing), deny (routed)'
  elif [[ "$*" == 'show added' ]]; then
    printf '%s\n' "$expected_firewall_rules"
  else
    return 1
  fi
}
readback_firewall
sysctl() { [[ "$*" == '-n net.ipv4.ip_forward' ]] && printf '1\n'; }
iptables() {
  [[ "$*" == '-w 5 -S FORWARD' ]] || return 1
  printf '%s\n' \
    '-A FORWARD -m comment --comment "kube-router netpol" -j KUBE-ROUTER-FORWARD' \
    '-A FORWARD -m mark --mark 0x20000/0x20000 -j ACCEPT' \
    '-A FORWARD -j ufw-before-forward'
}
readback_k3s_forwarding
if (
  iptables() {
    printf '%s\n' \
      '-A FORWARD -j ufw-before-forward' \
      '-A FORWARD -j KUBE-ROUTER-FORWARD' \
      '-A FORWARD -m mark --mark 0x20000/0x20000 -j ACCEPT'
  }
  readback_k3s_forwarding >/dev/null 2>&1
); then
  fail 'forwarding readback accepted UFW before Kubernetes NetworkPolicy'
fi
if (
  ufw() {
    if [[ "$*" == 'status verbose' ]]; then
      printf '%s\n' \
        'Status: active' \
        'Default: deny (incoming), allow (outgoing), deny (routed)'
    elif [[ "$*" == 'show added' ]]; then
      printf '%s\n' "$expected_firewall_rules" 'ufw allow 9999/tcp'
    else
      return 1
    fi
  }
  readback_firewall >/dev/null 2>&1
); then
  fail 'firewall readback accepted an extra rule'
fi

source <(sed -n '/^download_integrity_artifact() {$/,/^}$/p' "$prepare_host")
fixture_source="$temporary_directory/package"
fixture_archive="$temporary_directory/package.tgz"
fixture_output="$temporary_directory/downloaded.tgz"
fixture_lock="$temporary_directory/components.lock.json"
mkdir -p "$fixture_source/package"
printf '{"name":"fixture","version":"1.0.0"}\n' >"$fixture_source/package/package.json"
tar -czf "$fixture_archive" -C "$fixture_source" package
fixture_integrity="sha512-$(openssl dgst -sha512 -binary "$fixture_archive" | base64 -w0)"
jq -n --arg url "file://$fixture_archive" --arg integrity "$fixture_integrity" '
  {artifacts:[{name:"fixture",url:$url,integrity:$integrity}]}
' >"$fixture_lock"
lock_file=$fixture_lock
curl() {
  local source_file='' output_file=''
  while (($# > 0)); do
    case "$1" in
      file://*) source_file=${1#file://}; shift ;;
      --output) output_file=$2; shift 2 ;;
      *) shift ;;
    esac
  done
  cp -- "$source_file" "$output_file"
}
download_integrity_artifact fixture "$fixture_output"
cmp -s "$fixture_archive" "$fixture_output" || fail 'verified artifact bytes changed'
jq '.artifacts[0].integrity = "sha512-invalid"' "$fixture_lock" >"$fixture_lock.invalid"
lock_file="$fixture_lock.invalid"
if (download_integrity_artifact fixture "$fixture_output" >/dev/null 2>&1); then
  fail 'integrity downloader accepted a mismatched SHA-512'
fi

printf 'Kodex host hardening contract tests passed\n'
