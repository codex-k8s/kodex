-- name: TurnAttemptFinish
UPDATE control_plane.turn_attempts
SET
    state = @state,
    finished_at = @finished_at,
    outcome = @outcome
WHERE turn_id = @turn_id::uuid
  AND attempt = @attempt
  AND workload_id = @workload_id
  AND authority_generation = @authority_generation
  AND lease_fence = @lease_fence
  AND state IN ('QUEUED', 'CLAIMED')
