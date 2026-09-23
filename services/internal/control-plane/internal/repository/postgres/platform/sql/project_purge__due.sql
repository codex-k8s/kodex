-- name: project_purge__due :exec
WITH candidates AS MATERIALIZED (
    SELECT id FROM control_plane.projects
    WHERE lifecycle='TRASHED' AND purge_after<=statement_timestamp()
    ORDER BY purge_after,id FOR UPDATE SKIP LOCKED LIMIT @limit
), changed AS (
    UPDATE control_plane.projects project
    SET lifecycle='PURGE_PENDING', version=version+1, updated_at=statement_timestamp()
    FROM candidates WHERE project.id=candidates.id
    RETURNING project.id,project.organization_id,project.ref,project.deleted_by
), receipts AS (
INSERT INTO control_plane.project_purge_receipts
    (project_id,organization_id,project_ref,deleted_by,state)
SELECT id,organization_id,ref,deleted_by,'PENDING' FROM changed
RETURNING project_id
), cancelled_tasks AS (
UPDATE control_plane.session_archive_tasks task
SET state='CANCELLED',safe_error_code='PROJECT_PURGE_RETENTION',
    completed_at=statement_timestamp(),updated_at=statement_timestamp()
FROM changed WHERE task.project_id=changed.id AND task.state='READY'
RETURNING task.id
)
SELECT (SELECT count(*) FROM receipts),(SELECT count(*) FROM cancelled_tasks)
