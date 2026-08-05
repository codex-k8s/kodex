-- name: ScheduledRunWaitOwner
UPDATE control_plane.scheduled_runs
SET state = 'WAITING_OWNER',
    outcome = @outcome,
    result_artifact_id = @result_artifact_id::uuid
WHERE occurrence_id = @occurrence_id::uuid
  AND attempt = @attempt
  AND state IN ('CLAIMED', 'CONTINUATION')
