-- name: workflow_dispatch__finish_turn :exec
UPDATE control_plane.session_turns
SET state=@state,completed_at=clock_timestamp()
WHERE organization_id=@organization_id::uuid AND run_id=@root_run_id::uuid
    AND @state::text IN ('COMPLETED','FAILED')
    AND (state IN ('QUEUED','RUNNING') OR (state='CANCELLED' AND @state='FAILED'));
