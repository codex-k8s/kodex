-- name: RuntimeRestoreOperationConsume :exec
UPDATE control_plane.runtime_restore_operations
SET consumed_generation = @generation,
    updated_at = @updated_at
WHERE id = @id::uuid
  AND generation = @generation
  AND consumed_generation < @generation
  AND revoked_generation < @generation
  AND target_turn_id = @target_turn_id::uuid
  AND target_attempt = @target_attempt
  AND source_authority_sha256 = @source_authority_sha256;
