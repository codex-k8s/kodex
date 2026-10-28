#!/usr/bin/env bash
# Связывает runtime revision backup unit с тем же immutable image из release lock.
set -euo pipefail
fail() { printf 'Backup release pin failed: %s\n' "$*" >&2; exit 1; }
[[ $# -eq 2 && -f "$1" && -f "$2" ]] || fail 'lock and rendered manifest are required'
lock=$1 render=$2
# Отдельные извлечения избегают неоднозначного контекста jq при проверке pull_ref.
digest=$(jq -er '[.images[] | select(.component == "backup-controller")] |
  select(length == 1) | .[0].digest |
  select(test("^sha256:[a-f0-9]{64}$") and . != ("sha256:" + ("0" * 64)))' "$lock") || fail 'exact backup digest is required'
pull_ref=$(jq -er '[.images[] | select(.component == "backup-controller")] | select(length == 1) | .[0].pull_ref' "$lock")
[[ "$pull_ref" == *"/backup-controller@$digest" ]] || fail 'backup pull reference differs from digest'
count=$(yq -o=json -I=0 'select(.kind == "Deployment" and .metadata.name == "backup-controller")' "$render" |
  jq -s '[.[] | .spec.template.spec.containers[] | select(.name == "backup-controller")] | length')
[[ "$count" == 1 ]] || fail 'one backup container is required'
actual=$(yq -r 'select(.kind == "Deployment" and .metadata.name == "backup-controller") |
  .spec.template.spec.containers[] | select(.name == "backup-controller") | .image' "$render")
[[ "$actual" == "$pull_ref" ]] || fail 'rendered backup image differs from lock'
count=$(yq -o=json -I=0 'select(.kind == "Deployment" and .metadata.name == "backup-controller")' "$render" |
  jq -s '[.[] | .spec.template.spec.containers[] | select(.name == "backup-controller") |
    .env[] | select(.name == "BACKUP_CONTROLLER_RELEASE_REVISION")] | length')
[[ "$count" == 1 ]] || fail 'one backup release revision is required'
BACKUP_CONTROLLER_DIGEST="$digest" yq -i '
  with(select(.kind == "Deployment" and .metadata.name == "backup-controller");
    (.spec.template.spec.containers[] | select(.name == "backup-controller") |
      .env[] | select(.name == "BACKUP_CONTROLLER_RELEASE_REVISION").value) = strenv(BACKUP_CONTROLLER_DIGEST))
' "$render"
