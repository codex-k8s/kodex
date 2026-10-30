-- name: workspace_archive__candidates :many
SELECT s.id::text
FROM control_plane.sessions s
JOIN control_plane.session_workspace_storage w ON w.session_id=s.id AND w.organization_id=s.organization_id AND w.project_id=s.project_id
JOIN control_plane.projects p ON p.id=w.project_id AND p.organization_id=w.organization_id
WHERE w.organization_id=@organization_id::uuid
 AND p.lifecycle IN ('ACTIVE','TRASHED')
 AND ((w.state='CHECKPOINT_PENDING' AND w.writer_released AND
 (NOT EXISTS(SELECT 1 FROM control_plane.session_archive_tasks t WHERE t.workspace_id=w.id AND t.workspace_generation=w.generation AND t.kind='SNAPSHOT')
   OR EXISTS(SELECT 1 FROM control_plane.session_archive_tasks t WHERE t.workspace_id=w.id AND t.workspace_generation=w.generation AND t.kind='SNAPSHOT'
     AND (t.state='READY' AND t.attempt<t.maximum_attempts AND t.available_at<=clock_timestamp() OR t.state='CLAIMED' AND t.lease_expires_at<=clock_timestamp()))))
 OR (w.state IN('RESTORE_READY','RESTORING') AND NOT w.writer_released
   AND EXISTS(SELECT 1 FROM control_plane.session_archive_tasks t WHERE t.workspace_id=w.id AND t.workspace_generation=w.generation AND t.kind='RESTORE'
     AND (t.state='READY' AND t.attempt<t.maximum_attempts AND t.available_at<=clock_timestamp() OR t.state='CLAIMED' AND t.lease_expires_at<=clock_timestamp()))))
ORDER BY w.updated_at,w.id
FOR UPDATE OF s SKIP LOCKED
LIMIT @limit;
