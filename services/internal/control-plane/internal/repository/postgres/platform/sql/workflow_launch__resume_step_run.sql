-- name: workflow_launch__resume_step_run :exec
UPDATE control_plane.runs SET state='RUNNING',finished_at=NULL,updated_at=clock_timestamp(),version=version+1
WHERE id=@run_id::uuid AND organization_id=@organization_id::uuid AND id<>root_run_id AND state='SUCCEEDED';
