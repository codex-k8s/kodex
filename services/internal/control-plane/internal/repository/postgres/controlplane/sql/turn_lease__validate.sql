SELECT
    turn_id::text,
    token_hash,
    workload_id,
    expires_at,
    fence
FROM control_plane.turn_leases
WHERE turn_id = @turn_id::uuid
  AND token_hash = @token_hash
  AND workload_id = @workload_id
  AND expires_at > @now
FOR UPDATE
