-- name: project_purge__cancel_ready_archive_tasks :exec
UPDATE control_plane.session_archive_tasks
SET state='CANCELLED',safe_error_code='PROJECT_PURGE_REQUESTED',
    completed_at=statement_timestamp(),updated_at=statement_timestamp()
WHERE organization_id=@organization_id::uuid AND project_id=@project_id::uuid
  AND state='READY'
