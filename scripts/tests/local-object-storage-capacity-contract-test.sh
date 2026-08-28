#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
statefulset="$root/deploy/k8s/base/local-object-storage/statefulset.yaml"
bootstrap="$root/deploy/k8s/base/local-object-storage/bucket-bootstrap-job.yaml"
deployer="$root/tools/dev/deploy-local.sh"

fail() {
  printf 'Local object storage capacity contract failed: %s\n' "$*" >&2
  exit 1
}

volume_size=$(yq -r '
  select(.kind == "StatefulSet" and .metadata.name == "seaweedfs") |
  .spec.template.spec.containers[] | select(.name == "seaweedfs") |
  .args[] | select(test("^-master\\.volumeSizeLimitMB=")) |
  sub("^-master.volumeSizeLimitMB="; "")
' "$statefulset")
volume_max=$(yq -r '
  select(.kind == "StatefulSet" and .metadata.name == "seaweedfs") |
  .spec.template.spec.containers[] | select(.name == "seaweedfs") |
  .args[] | select(test("^-volume\\.max=")) |
  sub("^-volume.max="; "")
' "$statefulset")

[[ "$volume_size" =~ ^[0-9]+$ && "$volume_max" =~ ^[0-9]+$ ]] ||
  fail 'SeaweedFS volume capacity arguments are absent'
((volume_size * volume_max <= 4096)) ||
  fail 'declared SeaweedFS volume capacity exceeds the local PVC budget'
((volume_max >= 32)) ||
  fail 'SeaweedFS cannot allocate independent collections for all required buckets'

for marker in 'put-object --bucket "$bucket"' 'get-object --bucket "$bucket"' \
  'delete-object --bucket "$bucket"' 'list-object-versions --bucket "$bucket"'; do
  grep -Fq "$marker" "$bootstrap" || fail "bootstrap has no required write-path check: $marker"
done

grep -Fq 'discover_local_object_storage_secret' "$deployer" ||
  fail 'readback mode cannot discover the content-addressed object storage Secret'
grep -Fq 'multiple local object storage Secrets are present' "$deployer" ||
  fail 'readback mode does not reject ambiguous object storage credentials'

printf 'Local object storage capacity contract passed.\n'
