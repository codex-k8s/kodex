-- name: target_cutover__verify_restore :one
UPDATE control_plane.legacy_data_cutovers
SET restore_verified_at = coalesce(restore_verified_at, transaction_timestamp())
WHERE plan_id = @plan_id
  AND plan_sha256 = @plan_sha256
  AND source_sha256 = @source_sha256
  AND target_sha256 = @target_sha256
  AND backup_sha256 = @backup_sha256
  AND manifest_sha256 = @manifest_sha256
  AND state = 'PREPARED'
RETURNING plan_id, plan_sha256, source_sha256, target_sha256,
          backup_sha256, manifest_sha256, state, restore_verified_at IS NOT NULL;
