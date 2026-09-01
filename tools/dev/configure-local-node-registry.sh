#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex local node registry configuration failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    'Usage: configure-local-node-registry.sh --mode apply|readback' \
    '  --context <exact-context> --material-directory <path>' \
    '  --promoted-pull-host <dns>' >&2
}

mode=""
context=""
material_directory=""
promoted_pull_host=""
while (($# > 0)); do
  case "$1" in
    --mode) mode=${2:-}; shift 2 ;;
    --context) context=${2:-}; shift 2 ;;
    --material-directory) material_directory=${2:-}; shift 2 ;;
    --promoted-pull-host) promoted_pull_host=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

case "$mode" in apply|readback) ;; *) fail 'mode is invalid' ;; esac
[[ -n "$context" && "$(kubectl config current-context)" == "$context" ]] ||
  fail 'Kubernetes context mismatch'
[[ "${context,,}" != *prod* && "${context,,}" != *production* ]] ||
  fail 'production context is forbidden'
[[ -d "$material_directory" && ! -L "$material_directory" ]] ||
  fail 'material directory is invalid'
[[ "$promoted_pull_host" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ &&
  "$promoted_pull_host" == *.* ]] || fail 'promoted pull host is invalid'
for command_name in jq kubectl sha256sum stat sudo systemctl yq; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done

username_file="$material_directory/node-pull/username"
password_file="$material_directory/node-pull/password"
ca_file="$material_directory/node-pull/ca.crt"
certificate_file="$material_directory/node-pull/client.crt"
private_key_file="$material_directory/node-pull/client.key"
for input_file in "$username_file" "$password_file" "$ca_file" "$certificate_file" "$private_key_file"; do
  [[ -f "$input_file" && -s "$input_file" && ! -L "$input_file" ]] ||
    fail 'node pull material is incomplete'
  input_mode=$(stat -c '%a' "$input_file")
  (((8#$input_mode & 0077) == 0)) || fail 'node pull material permissions are too broad'
done

username=$(<"$username_file")
password=$(<"$password_file")
[[ -n "$username" && "$username" != *$'\n'* && "$username" != *$'\r'* ]] ||
  fail 'node pull username is invalid'
[[ ${#password} -ge 32 && "$password" != *$'\n'* && "$password" != *$'\r'* ]] ||
  fail 'node pull password is invalid'

system_directory=/etc/rancher/k3s/kodex-registry
system_ca="$system_directory/ca.crt"
system_certificate="$system_directory/client.crt"
system_private_key="$system_directory/client.key"
registry_configuration=/etc/rancher/k3s/registries.yaml
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"; unset password' EXIT

existing_json='{}'
if sudo test -f "$registry_configuration"; then
  sudo yq -o=json "$registry_configuration" |
    jq -c . >"$temporary_directory/existing.json"
  existing_json=$(jq -c 'if type == "object" then . else {} end' "$temporary_directory/existing.json")
fi
expected_json=$(jq -cn --argjson existing "$existing_json" --arg host "$promoted_pull_host" \
  --arg username "$username" --arg password "$password" --arg ca "$system_ca" \
  --arg certificate "$system_certificate" --arg private_key "$system_private_key" '
    $existing |
    .mirrors = ((.mirrors // {}) + {($host):{endpoint:[("https://" + $host)]}}) |
    .configs = ((.configs // {}) + {($host):{
      auth:{username:$username,password:$password},
      tls:{ca_file:$ca,cert_file:$certificate,key_file:$private_key}
    }})
  ')
printf '%s\n' "$expected_json" | yq -P >"$temporary_directory/registries.yaml"
chmod 0600 "$temporary_directory/registries.yaml"

changed=false
if ! sudo test -f "$registry_configuration"; then
  changed=true
else
  actual=$(sudo yq -o=json "$registry_configuration" | jq -cS .)
  expected=$(jq -cS . <<<"$expected_json")
  [[ "$actual" == "$expected" ]] || changed=true
fi
for pair in "$ca_file:$system_ca" "$certificate_file:$system_certificate" "$private_key_file:$system_private_key"; do
  source_path=${pair%%:*}
  target_path=${pair#*:}
  source_digest=$(sha256sum "$source_path" | awk '{print $1}')
  target_digest=""
  if sudo test -f "$target_path"; then
    target_digest=$(sudo sha256sum "$target_path" | awk '{print $1}')
  fi
  if [[ "$source_digest" != "$target_digest" ]]; then
    changed=true
  fi
done

if [[ "$mode" == apply && "$changed" == true ]]; then
  sudo install -d -m 0700 "$system_directory"
  sudo install -m 0600 "$ca_file" "$system_ca"
  sudo install -m 0600 "$certificate_file" "$system_certificate"
  sudo install -m 0600 "$private_key_file" "$system_private_key"
  sudo install -d -m 0700 /etc/rancher/k3s
  sudo install -m 0600 "$temporary_directory/registries.yaml" "$registry_configuration"
  sudo systemctl restart k3s
fi

for attempt in $(seq 1 120); do
  kubectl get --raw=/readyz >/dev/null 2>&1 && break
  ((attempt < 120)) || fail 'Kubernetes API did not recover after registry configuration'
  sleep 2
done
sudo test -f "$registry_configuration" || fail 'K3s registry configuration is absent'
actual_host=$(sudo yq -o=json "$registry_configuration" | jq -cS --arg host "$promoted_pull_host" '
  {mirror:.mirrors[$host],config:.configs[$host]}
')
expected_host=$(jq -cS --arg host "$promoted_pull_host" '
  {mirror:.mirrors[$host],config:.configs[$host]}
' <<<"$expected_json")
[[ "$actual_host" == "$expected_host" ]] || fail 'K3s promoted pull configuration mismatch'
for pair in "$ca_file:$system_ca" "$certificate_file:$system_certificate" "$private_key_file:$system_private_key"; do
  source_path=${pair%%:*}
  target_path=${pair#*:}
  source_digest=$(sha256sum "$source_path" | awk '{print $1}')
  target_digest=$(sudo sha256sum "$target_path" | awk '{print $1}')
  [[ "$source_digest" == "$target_digest" ]] ||
    fail 'K3s promoted pull TLS material mismatch'
done

printf 'Kodex local node registry configuration completed: %s\n' "$mode"
