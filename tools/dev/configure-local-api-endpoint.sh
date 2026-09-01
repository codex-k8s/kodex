#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex local Kubernetes API endpoint configuration failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf 'Usage: %s --context <exact-context> --mode apply|readback [--address <private-IPv4>]\n' "$0" >&2
}

context=""
mode=""
address=${KODEX_DEV_KUBERNETES_API_ADDRESS:-10.254.254.1}
interface=kodex-api0
service_name=kodex-local-api-address.service
helper_path=/usr/local/libexec/kodex-local-api-address
unit_path=/etc/systemd/system/$service_name
k3s_config=/etc/rancher/k3s/config.yaml

while (($# > 0)); do
  case "$1" in
    --context) context=${2:-}; shift 2 ;;
    --mode) mode=${2:-}; shift 2 ;;
    --address) address=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$context" ]] || fail 'exact Kubernetes context is required'
case "$mode" in apply|readback) ;; *) fail 'mode is invalid' ;; esac
for command_name in ip jq kubectl python3 sudo systemctl yq; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
sudo -n true >/dev/null 2>&1 || fail 'passwordless sudo is required'
[[ "$(kubectl config current-context)" == "$context" ]] || fail 'Kubernetes context mismatch'
python3 - "$address" <<'PY' || fail 'API address must be a private non-loopback IPv4 address'
import ipaddress
import sys

address = ipaddress.ip_address(sys.argv[1])
if address.version != 4 or not address.is_private or address.is_loopback or address.is_unspecified:
    raise SystemExit(1)
PY

temporary_directory=$(mktemp -d)
cleanup() { rm -rf -- "$temporary_directory"; }
trap cleanup EXIT

helper_candidate=$temporary_directory/kodex-local-api-address
cat >"$helper_candidate" <<EOF
#!/usr/bin/env bash
set -euo pipefail

interface=$interface
address=$address
if ip link show dev "\$interface" >/dev/null 2>&1; then
  ip -d link show dev "\$interface" | grep -Eq '(^|[[:space:]])dummy([[:space:]]|$)' || {
    printf 'Kodex local API address failed: %s exists and is not a dummy interface\n' "\$interface" >&2
    exit 1
  }
else
  ip link add name "\$interface" type dummy
fi
ip address replace "\$address/32" dev "\$interface"
ip link set dev "\$interface" up
EOF
chmod 0755 "$helper_candidate"

unit_candidate=$temporary_directory/$service_name
cat >"$unit_candidate" <<EOF
[Unit]
Description=Stable host-only address for the local Kodex Kubernetes API
After=local-fs.target
Before=k3s.service

[Service]
Type=oneshot
ExecStart=$helper_path
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
EOF
chmod 0644 "$unit_candidate"

config_candidate=$temporary_directory/config.yaml
sudo cat "$k3s_config" | KODEX_LOCAL_API_ADDRESS="$address" yq -o=yaml '
  ."advertise-address" = strenv(KODEX_LOCAL_API_ADDRESS) |
  ."tls-san" = (((."tls-san" // []) + [strenv(KODEX_LOCAL_API_ADDRESS)]) | unique)
' >"$config_candidate" || fail 'cannot render k3s API configuration'
chmod 0600 "$config_candidate"

if [[ "$mode" == apply ]]; then
  assigned_elsewhere=$(ip -4 -o address show | awk -v address="$address" -v interface="$interface" '
    $2 != interface {
      split($4, value, "/")
      if (value[1] == address) print $2
    }
  ')
  [[ -z "$assigned_elsewhere" ]] || fail "API address is already assigned to another interface: $assigned_elsewhere"

  unit_changed=false
  config_changed=false
  if ! sudo test -f "$helper_path" || ! sudo cmp -s "$helper_candidate" "$helper_path"; then
    sudo install -d -m 0755 "$(dirname -- "$helper_path")"
    sudo install -m 0755 "$helper_candidate" "$helper_path"
    unit_changed=true
  fi
  if ! sudo test -f "$unit_path" || ! sudo cmp -s "$unit_candidate" "$unit_path"; then
    sudo install -m 0644 "$unit_candidate" "$unit_path"
    unit_changed=true
  fi
  if ! sudo cmp -s "$config_candidate" "$k3s_config"; then
    sudo install -m 0600 "$config_candidate" "$k3s_config"
    config_changed=true
  fi
  if [[ "$unit_changed" == true ]]; then
    sudo systemctl daemon-reload
  fi
  sudo systemctl enable "$service_name" >/dev/null
  sudo systemctl restart "$service_name"
  if [[ "$config_changed" == true ]]; then
    sudo systemctl restart k3s
  fi
fi

sudo systemctl is-enabled --quiet "$service_name" || fail 'stable API address service is not enabled'
sudo systemctl is-active --quiet "$service_name" || fail 'stable API address service is not active'
ip -d link show dev "$interface" | grep -Eq '(^|[[:space:]])dummy([[:space:]]|$)' ||
  fail 'stable API interface is not a dummy interface'
ip -4 -o address show dev "$interface" | awk -v expected="$address/32" '$4 == expected { found = 1 } END { exit !found }' ||
  fail 'stable API address is absent from the dummy interface'
sudo yq -o=json "$k3s_config" | jq -e --arg address "$address" '
  .["advertise-address"] == $address and
  ((.["tls-san"] | index($address)) != null)
' >/dev/null || fail 'k3s does not advertise the stable API address'

api_ready=false
endpoint_ready=false
for _ in $(seq 1 120); do
  if kubectl get --raw=/readyz >/dev/null 2>&1; then
    api_ready=true
    if kubectl -n default get endpointslice -l kubernetes.io/service-name=kubernetes -o json |
      jq -e --arg address "$address" '
        ([.items[] |
          select(.addressType == "IPv4") |
          .endpoints[] |
          select(.conditions.ready != false) |
          .addresses[]] |
        unique) == [$address] and
        ([.items[].ports[] | select(.protocol == "TCP") | .port] | unique) == [6443]
      ' >/dev/null; then
      endpoint_ready=true
      break
    fi
  fi
  sleep 2
done
[[ "$api_ready" == true ]] || fail 'Kubernetes API did not become ready'
[[ "$endpoint_ready" == true ]] || fail 'Kubernetes EndpointSlice does not use the stable API address'

direct_kubeconfig=$temporary_directory/direct-kubeconfig.yaml
kubectl config view --raw --minify >"$direct_kubeconfig"
KODEX_LOCAL_API_ADDRESS="$address" yq -i '
  .clusters[0].cluster.server = "https://" + strenv(KODEX_LOCAL_API_ADDRESS) + ":6443"
' "$direct_kubeconfig"
KUBECONFIG="$direct_kubeconfig" kubectl get --raw=/readyz >/dev/null ||
  fail 'Kubernetes API is unreachable through the stable address with verified TLS'

printf 'Kodex local Kubernetes API endpoint configured: %s (%s)\n' "$mode" "$address"
