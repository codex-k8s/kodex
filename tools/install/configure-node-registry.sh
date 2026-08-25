#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex node registry configuration failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 --mode apply|readback --registry-host <dns>" \
    '  --username-file <path> --password-file <path>' \
    '  --promoted-pull-host <dns> --promoted-pull-username-file <path>' \
    '  --promoted-pull-password-file <path>' >&2
}

mode=""
registry_host=""
username_file=""
password_file=""
promoted_pull_host=""
promoted_pull_username_file=""
promoted_pull_password_file=""
while (($# > 0)); do
  case "$1" in
    --mode) mode="${2:-}"; shift 2 ;;
    --registry-host) registry_host="${2:-}"; shift 2 ;;
    --username-file) username_file="${2:-}"; shift 2 ;;
    --password-file) password_file="${2:-}"; shift 2 ;;
    --promoted-pull-host) promoted_pull_host="${2:-}"; shift 2 ;;
    --promoted-pull-username-file) promoted_pull_username_file="${2:-}"; shift 2 ;;
    --promoted-pull-password-file) promoted_pull_password_file="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ "$mode" == apply || "$mode" == readback ]] || fail 'mode is invalid'
[[ "$registry_host" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ && "$registry_host" == *.* ]] ||
  fail 'registry host is invalid'
[[ "$promoted_pull_host" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ &&
  "$promoted_pull_host" == *.* && "$promoted_pull_host" != "$registry_host" ]] ||
  fail 'promoted pull host is invalid'
for input_file in "$username_file" "$password_file" \
  "$promoted_pull_username_file" "$promoted_pull_password_file"; do
  [[ -f "$input_file" && -s "$input_file" && ! -L "$input_file" ]] ||
    fail 'registry credential file is invalid'
  input_mode=$(stat -c '%a' "$input_file")
  (((8#$input_mode & 0077) == 0)) || fail 'registry credential file permissions are too broad'
done
for command_name in jq kubectl systemctl yq; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
((EUID == 0)) || fail 'node registry configuration must run as root'

username=$(<"$username_file")
password=$(<"$password_file")
promoted_pull_username=$(<"$promoted_pull_username_file")
promoted_pull_password=$(<"$promoted_pull_password_file")
[[ "$username" != *$'\n'* && "$username" != *$'\r'* && -n "$username" &&
  "$promoted_pull_username" != *$'\n'* && "$promoted_pull_username" != *$'\r'* &&
  -n "$promoted_pull_username" ]] ||
  fail 'registry username is invalid'
[[ "$password" != *$'\n'* && "$password" != *$'\r'* && ${#password} -ge 32 ]] ||
  fail 'registry password is invalid'
[[ "$promoted_pull_password" != *$'\n'* &&
  "$promoted_pull_password" != *$'\r'* && ${#promoted_pull_password} -ge 32 ]] ||
  fail 'promoted pull password is invalid'

expected=$(mktemp)
trap 'rm -f -- "$expected"' EXIT
jq -n --arg host "$registry_host" --arg username "$username" --arg password "$password" \
  --arg pull_host "$promoted_pull_host" --arg pull_username "$promoted_pull_username" \
  --arg pull_password "$promoted_pull_password" '{
  mirrors:{
    ($host):{endpoint:[("https://" + $host)]},
    ($pull_host):{endpoint:[("https://" + $pull_host)]}
  },
  configs:{
    ($host):{auth:{username:$username,password:$password}},
    ($pull_host):{auth:{username:$pull_username,password:$pull_password}}
  }
}' | yq -P >"$expected"
chmod 0600 "$expected"

if [[ "$mode" == apply ]]; then
  install -d -m 0700 /etc/rancher/k3s
  install -m 0600 "$expected" /etc/rancher/k3s/registries.yaml
  systemctl restart k3s
fi

[[ -f /etc/rancher/k3s/registries.yaml ]] || fail 'K3s registry configuration is absent'
actual_registry_configuration=$(yq -o=json /etc/rancher/k3s/registries.yaml | jq -Sc .)
expected_registry_configuration=$(yq -o=json "$expected" | jq -Sc .)
[[ "$actual_registry_configuration" == "$expected_registry_configuration" ]] ||
  fail 'K3s registry configuration readback mismatch'
for attempt in $(seq 1 120); do
  kubectl get --raw=/readyz >/dev/null 2>&1 && break
  ((attempt < 120)) || fail 'Kubernetes API did not recover after registry configuration'
  sleep 2
done
unset password promoted_pull_password
printf 'Kodex node registry configuration completed: %s\n' "$mode"
