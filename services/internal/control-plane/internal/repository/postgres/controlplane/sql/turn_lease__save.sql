INSERT INTO control_plane.turn_leases (
    turn_id,
    token_hash,
    workload_id,
    authority_generation,
    attempt,
    expires_at,
    fence
) VALUES (
    @turn_id::uuid,
    @token_hash,
    @workload_id,
    @authority_generation,
    @attempt,
    @expires_at,
    @fence
)
ON CONFLICT (turn_id) DO UPDATE
SET
    token_hash = excluded.token_hash,
    workload_id = excluded.workload_id,
    authority_generation = excluded.authority_generation,
    attempt = excluded.attempt,
    expires_at = excluded.expires_at,
    fence = excluded.fence
WHERE control_plane.turn_leases.expires_at <= clock_timestamp()
