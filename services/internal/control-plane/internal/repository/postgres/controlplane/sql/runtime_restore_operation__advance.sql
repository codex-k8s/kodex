-- name: RuntimeRestoreOperationAdvance :exec
UPDATE control_plane.runtime_restore_operations
SET generation = @generation,
    revoked_generation = greatest(revoked_generation, @expected_generation),
    target_turn_id = @target_turn_id::uuid,
    target_attempt = @target_attempt,
    target_execution_id = @target_execution_id::uuid,
    updated_at = @updated_at
WHERE id = @id::uuid
  AND generation = @expected_generation
  AND consumed_generation <= @expected_generation
  AND revoked_generation >= @expected_generation;
