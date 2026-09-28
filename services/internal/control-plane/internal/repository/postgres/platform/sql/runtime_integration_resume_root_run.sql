-- name: runtime_integration_resume_root_run :exec
UPDATE control_plane.runs
SET state='RUNNING',version=version+1,updated_at=clock_timestamp()
WHERE id=@root_run_id::uuid AND organization_id=@organization_id::uuid
  AND state='WAITING_HUMAN'
  AND NOT EXISTS (
    SELECT 1 FROM control_plane.owner_gates gate
    WHERE gate.root_run_id=@root_run_id::uuid AND gate.state='OPEN'
  )
