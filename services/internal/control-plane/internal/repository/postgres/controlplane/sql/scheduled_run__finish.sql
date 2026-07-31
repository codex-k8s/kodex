-- name: ScheduledRunFinish
UPDATE control_plane.scheduled_runs
SET state = @state,
    outcome = @outcome,
    result_artifact_id = nullif(@result_artifact_id, '')::uuid,
    finished_at = @finished_at
WHERE occurrence_id = @occurrence_id::uuid
  AND attempt = @attempt
  AND state IN ('CLAIMED', 'WAITING_OWNER')
