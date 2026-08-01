-- name: TurnAttemptSave
INSERT INTO control_plane.turn_attempts (
    turn_id,
    attempt,
    workload_id,
    authority_generation,
    state,
    input_sha256,
    lease_fence,
    started_at
) VALUES (
    @turn_id::uuid,
    @attempt,
    @workload_id,
    @authority_generation,
    @state,
    @input_sha256,
    @lease_fence,
    @started_at
)
ON CONFLICT (turn_id, attempt) DO UPDATE
SET
    workload_id = excluded.workload_id,
    authority_generation = excluded.authority_generation,
    state = excluded.state,
    lease_fence = excluded.lease_fence,
    started_at = excluded.started_at
WHERE control_plane.turn_attempts.state = 'QUEUED'
  AND control_plane.turn_attempts.input_sha256 = excluded.input_sha256
