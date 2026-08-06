-- name: RuntimeRestoreOperationRevoke :exec
UPDATE control_plane.runtime_restore_operations
SET revoked_generation = greatest(revoked_generation, @generation),
    updated_at = @updated_at
WHERE id = @id::uuid
  AND generation = @generation
  AND revoked_generation <= @generation;
