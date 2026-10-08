-- name: runtime_deadline_reset_denied :exec
UPDATE control_plane.runs
SET execution_started_at=execution_started_at+interval '1 second',
    execution_deadline_at=execution_deadline_at+interval '1 second'
WHERE ref=@run_ref;
