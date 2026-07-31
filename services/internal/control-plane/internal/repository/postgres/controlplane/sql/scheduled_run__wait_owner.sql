-- name: ScheduledRunWaitOwner
UPDATE control_plane.scheduled_runs
SET state = 'WAITING_OWNER'
WHERE occurrence_id = @occurrence_id::uuid
  AND attempt = @attempt
  AND state = 'CLAIMED'
