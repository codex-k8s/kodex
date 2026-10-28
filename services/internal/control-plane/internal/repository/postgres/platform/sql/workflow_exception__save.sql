-- name: workflow_exception__save :exec
UPDATE control_plane.runs root SET workflow_exception_history=@history::jsonb,
    workflow_exception=NULLIF(@exception::jsonb,'null'::jsonb),version=version+1,updated_at=clock_timestamp()
WHERE root.organization_id=@organization_id::uuid AND root.id=@root_run_id::uuid AND root.root_run_id=root.id
    AND root.state IN ('QUEUED','RUNNING','WAITING_HUMAN');
