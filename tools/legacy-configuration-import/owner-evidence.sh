#!/usr/bin/env bash

# select_legacy_owner_evidence сохраняет исходное время наблюдения при exact
# retry. Изменение любого нетемпорального доказательства для того же plan tuple
# отклоняется, чтобы retry не скрывал drift credentials или admission policy.
select_legacy_owner_evidence() {
  local candidate_file=$1
  local live_configmap_file=$2
  local output_file=$3
  local plan_id=$4
  local source_root_reference=$5
  local source_root_sha256=$6
  local revision=$7

  [[ -r "$candidate_file" ]] || return 2
  if [[ ! -s "$live_configmap_file" ]] || ! jq -e \
    --arg plan_id "$plan_id" \
    --arg source_root_reference "$source_root_reference" \
    --arg source_root_sha256 "$source_root_sha256" \
    --arg revision "$revision" '
      .metadata.annotations["mattercodex.dev/legacy-plan-id"] == $plan_id and
      .metadata.annotations["mattercodex.dev/legacy-source-root-reference"] == $source_root_reference and
      .metadata.annotations["mattercodex.dev/legacy-source-root-sha256"] == $source_root_sha256 and
      .metadata.annotations["mattercodex.dev/legacy-release-revision"] == $revision
    ' "$live_configmap_file" >/dev/null 2>&1; then
    cp -- "$candidate_file" "$output_file"
    printf 'candidate\n'
    return 0
  fi

  local existing_file normalized_candidate normalized_existing
  existing_file="${output_file}.existing"
  if ! jq -er '.data["owner-evidence.json"]' "$live_configmap_file" >"$existing_file" ||
    ! jq -e '.schemaVersion == "mattercodex.legacy-data-owner-evidence.v1"' "$existing_file" >/dev/null ||
    ! jq -e '.schemaVersion == "mattercodex.legacy-data-owner-evidence.v1"' "$candidate_file" >/dev/null; then
    rm -f -- "$existing_file"
    return 2
  fi
  normalized_candidate=$(jq -S -c 'del(.archive.scannedAt, .provider.observedAt, .roleImage.promotedAt)' "$candidate_file") || return 2
  normalized_existing=$(jq -S -c 'del(.archive.scannedAt, .provider.observedAt, .roleImage.promotedAt)' "$existing_file") || return 2
  if [[ "$normalized_candidate" != "$normalized_existing" ]]; then
    rm -f -- "$existing_file"
    return 2
  fi
  mv -- "$existing_file" "$output_file"
  printf 'reused\n'
}
