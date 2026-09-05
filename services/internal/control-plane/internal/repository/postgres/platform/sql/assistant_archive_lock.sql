-- name: assistant_archive_lock :one
SELECT c.ref,c.title,c.title_source,c.title_revision,COALESCE(p.ref,''),s.ref,c.state,c.version,
       c.context_route,c.context_entity_kind,c.context_entity_ref,c.context_entity_name,
       c.context_entity_version,c.allowed_operations,c.created_at,c.updated_at,
       c.id::text,COALESCE(c.project_id::text,''),
       EXISTS(SELECT 1 FROM control_plane.runs r WHERE r.organization_id=c.organization_id AND r.session_id=c.session_id
              AND r.state NOT IN ('SUCCEEDED','FAILED','CANCELLED'))
FROM control_plane.assistant_conversations c
LEFT JOIN control_plane.projects p ON p.id=c.project_id
JOIN control_plane.sessions s ON s.id=c.session_id
WHERE c.organization_id=$1::uuid AND c.ref=$2 AND c.created_by=$3::uuid
FOR UPDATE OF c;
