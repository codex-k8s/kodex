-- name: source_cutover__verify_restore :one
UPDATE matter_codex_legacy_data_cutovers
SET restore_verified = true
WHERE plan_id = @plan_id
  AND plan_sha256 = @plan_sha256
  AND source_sha256 = @source_sha256
  AND target_sha256 = @target_sha256
  AND backup_sha256 = @backup_sha256
  AND manifest_sha256 = @manifest_sha256
  AND materialization_sha256 = @materialization_sha256
  AND materialization_count = @materialization_count
  AND state = 'PREPARED'
RETURNING plan_id, plan_sha256, source_sha256, target_sha256,
          backup_sha256, manifest_sha256, materialization_sha256,
          materialization_count, state, restore_verified;
