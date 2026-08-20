#!/usr/bin/env bash
set -euo pipefail

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
# shellcheck source=owner-evidence.sh
source "$script_directory/owner-evidence.sh"

temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT

candidate="$temporary_directory/candidate.json"
existing="$temporary_directory/existing.json"
configmap="$temporary_directory/configmap.json"
selected="$temporary_directory/selected.json"
plan_id=11111111-1111-4111-8111-111111111111
source_reference=22222222-2222-4222-8222-222222222222
source_sha=$(printf source | sha256sum | awk '{print $1}')
revision=$(printf 'a%.0s' {1..40})

jq -n --arg observed_at '2026-08-20T10:00:00Z' --arg policy_sha "$source_sha" '
  {schemaVersion:"mattercodex.legacy-data-owner-evidence.v1",
   archive:{scannedAt:$observed_at,scanEvidenceSha256:$policy_sha},
   provider:{observedAt:$observed_at},
   roleImage:{promotedAt:$observed_at,policySha256:$policy_sha}}
' >"$candidate"
jq --arg observed_at '2026-08-20T09:00:00Z' '
  .archive.scannedAt=$observed_at | .provider.observedAt=$observed_at | .roleImage.promotedAt=$observed_at
' "$candidate" >"$existing"
jq -n --arg plan_id "$plan_id" --arg source_reference "$source_reference" \
  --arg source_sha "$source_sha" --arg revision "$revision" --rawfile evidence "$existing" '
  {metadata:{annotations:{
    "mattercodex.dev/legacy-plan-id":$plan_id,
    "mattercodex.dev/legacy-source-root-reference":$source_reference,
    "mattercodex.dev/legacy-source-root-sha256":$source_sha,
    "mattercodex.dev/legacy-release-revision":$revision}},
   data:{"owner-evidence.json":$evidence}}
' >"$configmap"

selection=$(select_legacy_owner_evidence "$candidate" "$configmap" "$selected" \
  "$plan_id" "$source_reference" "$source_sha" "$revision")
[[ "$selection" == reused && "$(jq -r '.provider.observedAt' "$selected")" == '2026-08-20T09:00:00Z' ]]

selection=$(select_legacy_owner_evidence "$candidate" "$configmap" "$selected" \
  33333333-3333-4333-8333-333333333333 "$source_reference" "$source_sha" "$revision")
[[ "$selection" == candidate && "$(jq -r '.provider.observedAt' "$selected")" == '2026-08-20T10:00:00Z' ]]

jq '.roleImage.policySha256="changed"' "$candidate" >"$temporary_directory/drifted.json"
if select_legacy_owner_evidence "$temporary_directory/drifted.json" "$configmap" "$selected" \
  "$plan_id" "$source_reference" "$source_sha" "$revision" >/dev/null; then
  printf 'owner evidence drift was accepted\n' >&2
  exit 1
fi

printf 'owner evidence retry tests passed\n'
