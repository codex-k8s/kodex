-- name: runtime_integration_resume_child_run :exec
UPDATE control_plane.runs
SET state='RUNNING',finished_at=NULL,version=version+1,updated_at=clock_timestamp()
WHERE id=@run_id::uuid AND organization_id=@organization_id::uuid
  AND id<>root_run_id AND state='SUCCEEDED'
