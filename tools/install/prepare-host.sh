#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex host preparation failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 --mode preflight|apply|readback --server-public-ip <IPv4>" >&2
}

mode=""
server_public_ip=""
while (($# > 0)); do
  case "$1" in
    --mode) mode="${2:-}"; shift 2 ;;
    --server-public-ip) server_public_ip="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

case "$mode" in preflight|apply|readback) ;; *) fail 'mode is invalid' ;; esac
[[ "$server_public_ip" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]] ||
  fail 'server public IPv4 is invalid'
[[ "$(uname -s)" == Linux && "$(uname -m)" == x86_64 ]] ||
  fail 'only Linux x86_64 is supported by the bare-metal profile'
((EUID == 0)) || fail 'host preparation must run as root'

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
lock_file="$script_directory/components.lock.json"
pod_cidr=10.42.0.0/16
service_cidr=10.43.0.0/16

remove_legacy_firewall() {
  systemctl disable --now nftables >/dev/null 2>&1 || true
  if command -v nft >/dev/null 2>&1 && nft list table inet kodex_fw >/dev/null 2>&1; then
    nft delete table inet kodex_fw
  fi
}

configure_firewall() {
  remove_legacy_firewall
  ufw --force reset >/dev/null
  ufw default deny incoming >/dev/null
  ufw default allow outgoing >/dev/null
  ufw default deny routed >/dev/null
  ufw allow 22/tcp comment SSH >/dev/null
  ufw allow 80/tcp comment 'HTTP ingress' >/dev/null
  ufw allow 443/tcp comment 'HTTPS ingress' >/dev/null
  ufw allow from "$pod_cidr" comment 'K3s pods' >/dev/null
  ufw allow from "$service_cidr" comment 'K3s services' >/dev/null
  ufw route allow from "$pod_cidr" comment 'K3s pod forwarding' >/dev/null
  ufw route allow proto tcp to "$pod_cidr" port 80 comment 'K3s HTTP ingress DNAT' >/dev/null
  ufw route allow proto tcp to "$pod_cidr" port 443 comment 'K3s HTTPS ingress DNAT' >/dev/null
  ufw --force enable >/dev/null
}

readback_firewall() {
  local status
  command -v nft >/dev/null 2>&1 && nft list table inet kodex_fw >/dev/null 2>&1 &&
    fail 'legacy kodex_fw nftables policy is active'
  systemctl is-enabled --quiet nftables && fail 'nftables autoload remains enabled'
  status=$(ufw status verbose)
  grep -Fq 'Status: active' <<<"$status" || fail 'host firewall is inactive'
  grep -Fq 'Default: deny (incoming), allow (outgoing), deny (routed)' <<<"$status" ||
    fail 'host firewall defaults differ from the supported policy'
  for expected_rule in \
    '22/tcp' '80/tcp' '443/tcp' "$pod_cidr" "$service_cidr" \
    'K3s pod forwarding' 'K3s HTTP ingress DNAT' 'K3s HTTPS ingress DNAT'; do
    grep -Fq "$expected_rule" <<<"$status" || fail "host firewall rule is absent: $expected_rule"
  done
}

if [[ "$mode" == apply ]]; then
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq
  apt-get upgrade -y -qq
  apt-get install -y -qq \
    apache2-utils build-essential ca-certificates curl gh git iptables jq make \
    openssl python3 ripgrep rsync tar unzip uidmap ufw zstd
else
  command -v jq >/dev/null 2>&1 || fail 'jq is required'
fi
jq -e '.schemaVersion == 1 and (.artifacts | length) == 6 and (.charts | length) == 1' \
  "$lock_file" >/dev/null || fail 'component lock is invalid'

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
    "$url" --output "$output"
  actual_sha=$(sha256sum "$output" | awk '{print $1}')
  [[ "$actual_sha" == "$expected_sha" ]] || fail "artifact digest mismatch: $name"
}

if [[ "$mode" == apply ]]; then
  temporary_directory=$(mktemp -d)
  trap 'rm -rf -- "$temporary_directory"' EXIT
  download_artifact go "$temporary_directory/go.tar.gz"
  rm -rf -- /usr/local/go
  tar -xzf "$temporary_directory/go.tar.gz" -C /usr/local
  ln -sfn /usr/local/go/bin/go /usr/local/bin/go
  ln -sfn /usr/local/go/bin/gofmt /usr/local/bin/gofmt

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

  download_artifact k3s "$temporary_directory/k3s"
  install -m 0755 "$temporary_directory/k3s" /usr/local/bin/k3s
  ln -sfn /usr/local/bin/k3s /usr/local/bin/kubectl
  ln -sfn /usr/local/bin/k3s /usr/local/bin/crictl
  mkdir -p /etc/rancher/k3s /var/lib/rancher/k3s
  cat >/etc/rancher/k3s/config.yaml <<EOF
write-kubeconfig-mode: "0600"
secrets-encryption: true
tls-san:
  - "$server_public_ip"
kubelet-arg:
  - "max-pods=250"
EOF
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
  configure_firewall
  systemctl enable --now k3s
fi

systemctl is-active --quiet k3s || fail 'k3s service is not active'
for command_name in cosign go helm kubectl nsc yq; do
  command -v "$command_name" >/dev/null 2>&1 || fail "installed command is absent: $command_name"
done
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
api_ready=false
node_ready=false
for attempt in $(seq 1 120); do
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
readback_firewall
printf 'Kodex host preparation completed: %s\n' "$mode"
