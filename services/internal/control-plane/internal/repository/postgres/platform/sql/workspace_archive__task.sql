-- name: workspace_archive__task :one
SELECT t.id::text,t.ref,t.kind,t.state,t.content_generation,t.input_digest,
 CASE WHEN t.kind='RESTORE' THEN t.workspace_input #>> '{checkpoint,manifest,key}' ELSE COALESCE(t.object_key,'') END,
 COALESCE(t.object_version,''),
 t.attempt,t.maximum_attempts,t.generation,t.workspace_generation,t.workspace_input,t.lease_ref,t.fence_digest,t.lease_expires_at
FROM control_plane.session_archive_tasks t
WHERE t.organization_id=@organization_id::uuid AND t.workspace_id=@workspace_id::uuid
 AND t.workspace_generation=@workspace_generation AND t.kind=@kind
FOR UPDATE OF t;
