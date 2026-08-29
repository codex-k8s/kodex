-- name: runtime_secret_operation_fail :execrows
UPDATE control_plane.runtime_secret_operations
SET state = 'FAILED', terminal_error_code = @failure_code,
    terminal_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE id = @operation_id::uuid
  AND (
    (state = 'CLAIMED' AND @failure_code <> 'GRANT_EXPIRED'
      AND claimant_id = @claimant_id AND claim_generation = @claim_generation) OR
    (state = 'PREPARED' AND @failure_code = 'GRANT_EXPIRED'
      AND claimant_id IS NULL AND claim_generation = 0
      AND grant_expires_at <= clock_timestamp())
  );
