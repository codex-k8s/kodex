#!/usr/bin/env bash
set -euo pipefail

destination=${1:-}
[[ -n "$destination" ]] || { printf 'Kubeconfig destination is required\n' >&2; exit 1; }
token_file=/var/run/secrets/kubernetes.io/serviceaccount/token
ca_file=/var/run/secrets/kubernetes.io/serviceaccount/ca.crt
[[ -r "$token_file" && -r "$ca_file" ]] || { printf 'In-cluster ServiceAccount material is unavailable\n' >&2; exit 1; }
umask 077
token=$(<"$token_file")
ca_data=$(base64 -w0 "$ca_file")
{
  printf '%s\n' 'apiVersion: v1' 'kind: Config' 'clusters:' '- name: mattercodex-incluster' '  cluster:'
  printf '    certificate-authority-data: %s\n' "$ca_data"
  printf '%s\n' '    server: https://kubernetes.default.svc' 'users:' '- name: mattercodex-deployer' '  user:'
  printf '    token: %s\n' "$token"
  printf '%s\n' 'contexts:' '- name: mattercodex-incluster' '  context:' '    cluster: mattercodex-incluster' '    user: mattercodex-deployer' 'current-context: mattercodex-incluster'
} >"$destination"
unset token ca_data
printf 'In-cluster kubeconfig prepared\n'
