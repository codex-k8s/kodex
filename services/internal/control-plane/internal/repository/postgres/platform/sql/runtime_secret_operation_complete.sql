-- name: runtime_secret_operation_complete :execrows
UPDATE control_plane.runtime_secret_operations
SET state = 'COMPLETED', terminal_secret_snapshot = @terminal_secret_snapshot::jsonb,
    terminal_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE id = @operation_id::uuid AND state = 'CLAIMED'
  AND claimant_id = @claimant_id AND claim_generation = @claim_generation;
