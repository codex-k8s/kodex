-- name: runtime_secret_operation_consume :one
UPDATE control_plane.runtime_secret_operations
SET state = 'CLAIMED', claimant_id = @claimant_id,
    claim_generation = claim_generation + 1,
    claim_lease_deadline = @claim_lease_deadline,
    claimed_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE id = @operation_id::uuid
  AND state = 'PREPARED'
  AND grant_expires_at > clock_timestamp()
RETURNING claim_generation, claim_lease_deadline;
