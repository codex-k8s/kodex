-- name: TurnLeaseRenew
UPDATE control_plane.turn_leases
SET expires_at = @new_expires_at
WHERE turn_id = @turn_id::uuid
  AND token_hash = @token_hash
  AND workload_id = @workload_id
  AND authority_generation = @authority_generation
  AND attempt = @attempt
  AND fence = @fence
  AND expires_at > @now
RETURNING
    turn_id::text,
    token_hash,
    workload_id,
    authority_generation,
    attempt,
    expires_at,
    fence
