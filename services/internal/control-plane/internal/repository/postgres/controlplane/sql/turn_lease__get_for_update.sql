-- name: TurnLeaseGetForUpdate
SELECT
    turn_id::text,
    token_hash,
    workload_id,
    authority_generation,
    attempt,
    expires_at,
    fence
FROM control_plane.turn_leases
WHERE turn_id = @turn_id::uuid
FOR UPDATE
