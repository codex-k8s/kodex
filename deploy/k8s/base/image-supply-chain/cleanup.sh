#!/bin/sh
set -eu

registry_host="mattercodex-image-registry-admin.mattercodex-system.svc.cluster.local:5002"
ca_file="/var/run/secrets/mattercodex/image-registry/admin/ca.pem"
certificate_file="/var/run/secrets/mattercodex/image-registry/admin/client.crt"
key_file="/var/run/secrets/mattercodex/image-registry/admin/client.key"
username_file="/var/run/secrets/mattercodex/image-registry/admin/username"
password_file="/var/run/secrets/mattercodex/image-registry/admin/password"

regctl registry set "${registry_host}" --tls enabled \
  --cacert "$(cat "${ca_file}")" \
  --client-cert "$(cat "${certificate_file}")" \
  --client-key "$(cat "${key_file}")"
registry_username=$(tr -d '\r\n' <"${username_file}")
if [ -z "${registry_username}" ] || [ ! -s "${password_file}" ]; then
  echo "registry admin credentials are unavailable" >&2
  exit 1
fi
regctl registry login "${registry_host}" \
  --user "${registry_username}" --pass-stdin <"${password_file}" >/dev/null
repository_file="$(mktemp)"
regctl repo ls "${registry_host}" >"${repository_file}"
while IFS= read -r repository; do
  case "${repository}" in
    mattercodex/*) ;;
    *) exit 1 ;;
  esac
  tag_file="$(mktemp)"
  prune_file="$(mktemp)"
  regctl tag ls "${registry_host}/${repository}" >"${tag_file}"
  while IFS= read -r tag; do
    case "${tag}" in
      v[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]-[0-9a-f]*) ;;
      *) exit 1 ;;
    esac
  done <"${tag_file}"
  sort -r "${tag_file}" | awk 'NR > 3 { print }' >"${prune_file}"
  while IFS= read -r tag; do
    regctl tag rm "${registry_host}/${repository}:${tag}"
  done <"${prune_file}"
  rm -f "${tag_file}" "${prune_file}"
done <"${repository_file}"

regctl registry logout "${registry_host}" >/dev/null
rm -f "${repository_file}"
