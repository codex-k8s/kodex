-- name: project_purge__due_candidates :many
SELECT organization_id::text,id::text FROM control_plane.projects
WHERE lifecycle='TRASHED' AND purge_after<=statement_timestamp()
 AND control_plane.kubernetes_project_unreconciled_count(organization_id,id)=0
ORDER BY purge_after,id FOR UPDATE SKIP LOCKED LIMIT @limit;
