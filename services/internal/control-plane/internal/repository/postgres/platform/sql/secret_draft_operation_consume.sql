-- name: secret_draft_operation_consume :one
UPDATE control_plane.runtime_secret_draft_operations
SET state='CLAIMED',claimant_id=@claimant_id,claim_generation=claim_generation+1,
lease_deadline=LEAST(clock_timestamp()+interval '30 seconds',grant_expires_at),updated_at=clock_timestamp()
WHERE id=@operation_id::uuid AND state='PREPARED' AND grant_expires_at>clock_timestamp()
RETURNING claim_generation,lease_deadline;
