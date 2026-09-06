#!/bin/sh
set -eu
umask 077
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
export REGCTL_CONFIG="$temporary_directory/regctl.json"

registry_host="kodex-image-registry-admin.kodex-system.svc.cluster.local:5002"
identity_directory="/var/run/secrets/kodex/image-registry/admin"
username_file="/var/run/secrets/kodex/image-registry/admin/username"
password_file="/var/run/secrets/kodex/image-registry/admin/password"

if [ ! -s "${username_file}" ] || [ ! -s "${password_file}" ]; then
  echo "registry admin credentials are unavailable" >&2
  exit 1
fi
# Закреплённый regctl alpine не требует jq: awk читает только private files.
# JSON escaping выполняется внутри процесса; credentials не попадают в argv.
awk -v host="$registry_host" -v directory="$identity_directory" '
  function read_file(name, trim, value, line, status) {
    value = ""
    while ((status = (getline line < (directory "/" name))) > 0) value = value line "\n"
    close(directory "/" name)
    if (status < 0 || value == "") exit 1
    if (trim) gsub(/[\r\n]/, "", value)
    if (value == "") exit 1
    return value
  }
  function quote(value, result, i, char) {
    result = "\""
    for (i = 1; i <= length(value); i++) {
      char = substr(value, i, 1)
      if (char == "\\" || char == "\"") result = result "\\" char
      else if (char == "\n") result = result "\\n"
      else if (char == "\r") result = result "\\r"
      else if (char == "\t") result = result "\\t"
      else if (char ~ /[[:cntrl:]]/) exit 1
      else result = result char
    }
    return result "\""
  }
  BEGIN {
    printf "{\"version\":1,\"hosts\":{%s:{\"tls\":\"enabled\",", quote(host)
    printf "\"regcert\":%s,", quote(read_file("ca.pem", 0))
    printf "\"clientCert\":%s,", quote(read_file("client.crt", 0))
    printf "\"clientKey\":%s,", quote(read_file("client.key", 0))
    printf "\"user\":%s,", quote(read_file("username", 1))
    printf "\"pass\":%s}}}\n", quote(read_file("password", 1))
  }
' >"$REGCTL_CONFIG"
repository_file="$temporary_directory/repositories"
regctl repo ls "${registry_host}" >"${repository_file}"
while IFS= read -r repository; do
  case "${repository}" in
    kodex/*) ;;
    *) exit 1 ;;
  esac
  tag_file="$temporary_directory/tags"
  prune_file="$temporary_directory/prune"
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
