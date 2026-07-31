#!/bin/sh
set -eu

registry_host="mattercodex-image-registry.mattercodex-system.svc.cluster.local:5000"
ca_file="/var/run/config/mattercodex/image-registry/ca.pem"

regctl registry set "${registry_host}" --tls enabled --cacert "${ca_file}"
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
rm -f "${repository_file}"
