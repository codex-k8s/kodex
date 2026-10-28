-- name: workflow_dispatch__exception :exec
UPDATE control_plane.runs root
SET workflow_exception=@exception::jsonb,version=version+1,updated_at=clock_timestamp()
WHERE root.id=@root_run_id::uuid AND root.organization_id=@organization_id::uuid
    AND root.state IN ('RUNNING','WAITING_HUMAN') AND root.workflow_exception IS NULL;
