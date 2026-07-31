-- name: TurnAttemptGetForUpdate
SELECT
    turn_id::text,
    attempt,
    workload_id,
    authority_generation,
    state,
    input_sha256,
    lease_fence,
    started_at,
    coalesce(finished_at, 'epoch'::timestamptz),
    coalesce(outcome, '')
FROM control_plane.turn_attempts
WHERE turn_id = @turn_id::uuid
  AND attempt = @attempt
FOR UPDATE
