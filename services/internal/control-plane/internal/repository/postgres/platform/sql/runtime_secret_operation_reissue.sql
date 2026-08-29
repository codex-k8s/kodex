-- name: runtime_secret_operation_reissue :execrows
UPDATE control_plane.runtime_secret_operations
SET state = 'PREPARED', token_digest = @token_digest,
    grant_expires_at = @grant_expires_at, claimant_id = NULL,
    claim_lease_deadline = NULL, claimed_at = NULL, updated_at = clock_timestamp()
WHERE id = @operation_id::uuid
  AND state = 'PREPARED';
