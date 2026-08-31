#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex host reset failed: %s\n' "$*" >&2
  exit 1
}

confirmation=""
while (($# > 0)); do
  case "$1" in
    --confirm-destroy) confirmation="${2:-}"; shift 2 ;;
    --help)
      printf 'Usage: %s --confirm-destroy DESTROY-KODEX-HOST\n' "$0" >&2
      exit 0
      ;;
    *) fail "unsupported argument: $1" ;;
  esac
done
[[ "$confirmation" == DESTROY-KODEX-HOST ]] || fail 'explicit destroy confirmation is required'
((EUID == 0)) || fail 'host reset must run as root'

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
"$script_directory/configure-ipv6-ingress-bridge.sh" --mode apply

systemctl disable --now kodex-local-api-address.service >/dev/null 2>&1 || true
rm -f -- \
  /etc/systemd/system/kodex-local-api-address.service \
  /usr/local/libexec/kodex-local-api-address
ip link delete kodex-api0 >/dev/null 2>&1 || true

if [[ -x /usr/local/bin/k3s-killall.sh ]]; then
  /usr/local/bin/k3s-killall.sh >/dev/null 2>&1 || true
fi
systemctl disable --now k3s >/dev/null 2>&1 || true
for mount_path in $(findmnt -rn -o TARGET | awk '
  /^\/var\/lib\/kubelet\// || /^\/run\/k3s\// || /^\/var\/lib\/rancher\/k3s\// {print length, $0}
' | sort -rn | cut -d' ' -f2-); do
  umount -l "$mount_path" >/dev/null 2>&1 || true
done
systemctl reset-failed k3s >/dev/null 2>&1 || true

# Прежний профиль сохранял эту таблицу в /etc/nftables.conf. Она выполняется
# раньше цепочек Kubernetes/UFW и может незаметно блокировать ingress DNAT.
systemctl disable --now nftables >/dev/null 2>&1 || true
if command -v nft >/dev/null 2>&1 && nft list table inet kodex_fw >/dev/null 2>&1; then
  nft delete table inet kodex_fw
fi

for path in \
  /etc/cni/net.d \
  /etc/rancher/k3s \
  /run/flannel \
  /run/k3s \
  /var/lib/cni \
  /var/lib/kubelet \
  /var/lib/rancher/k3s \
  /var/log/pods \
  /var/log/containers; do
  [[ "$path" == /* && "$path" != / ]] || fail 'unsafe reset path'
  rm -rf --one-file-system -- "$path"
done
rm -f -- /etc/systemd/system/k3s.service /etc/systemd/system/multi-user.target.wants/k3s.service
systemctl daemon-reload

for interface in cni0 flannel.1 flannel-v6.1; do
  ip link delete "$interface" >/dev/null 2>&1 || true
done
command -v iptables-save >/dev/null 2>&1 && iptables-save | grep -q 'KUBE-\|CNI-\|K3S-' && {
  printf 'Kodex host reset failed: stale Kubernetes firewall chains remain\n' >&2
  exit 1
} || true
command -v nft >/dev/null 2>&1 && nft list table inet kodex_fw >/dev/null 2>&1 &&
  fail 'legacy kodex_fw nftables policy remains active'
printf 'Kodex host reset completed; all Kubernetes workloads and persistent data were removed\n'
