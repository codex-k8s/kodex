#!/usr/bin/env bash
set -euo pipefail

# Публикует только новый immutable revision path с Vault KV CAS=0. Переключение
# deployment выполняется отдельным Git PR, который одновременно pin-ит path,
# KV version, manifest revision и ожидаемый digest readback.
usage() {
  echo "usage: mapping-revision.sh publish|verify <environment> <revision> <manifest-file>" >&2
  exit 2
}

[[ $# -eq 4 ]] || usage
action=$1
environment=$2
revision=$3
manifest_file=$4
[[ "$environment" =~ ^(staging|production)$ ]] || usage
[[ "$revision" =~ ^(staging|production)-r[1-9][0-9]*$ ]] || usage
[[ -f "$manifest_file" ]] || { echo "mapping manifest is unavailable" >&2; exit 1; }

manifest_revision=$(jq -er '.revision' "$manifest_file")
[[ "$manifest_revision" == "$revision" ]] || { echo "mapping revision mismatch" >&2; exit 1; }
manifest_sha256=$(sha256sum "$manifest_file" | awk '{print $1}')
vault_path="kv/mattercodex/interaction-gateway/mapping/${environment}/revisions/${revision}"

case "$action" in
  publish)
    vault kv put -cas=0 "$vault_path" \
      "manifest.yaml=@${manifest_file}" \
      "manifest.sha256=${manifest_sha256}" \
      "revision=${revision}" >/dev/null
    ;;
  verify) ;;
  *) usage ;;
esac

readback_file=$(mktemp)
trap 'rm -f "$readback_file"' EXIT
vault kv get -format=json "$vault_path" >"$readback_file"
readback_version=$(jq -er '.data.metadata.version' "$readback_file")
readback_revision=$(jq -er '.data.data.revision' "$readback_file")
readback_sha256=$(jq -er '.data.data["manifest.sha256"]' "$readback_file")
readback_manifest_sha256=$(jq -er '.data.data["manifest.yaml"]' "$readback_file" | sha256sum | awk '{print $1}')
[[ "$readback_version" == "1" && "$readback_revision" == "$revision" &&
   "$readback_sha256" == "$manifest_sha256" && "$readback_manifest_sha256" == "$manifest_sha256" ]] || {
  echo "immutable mapping readback mismatch" >&2
  exit 1
}
echo "mapping revision verified: path=${vault_path} version=1 revision=${revision} sha256=${manifest_sha256}"
