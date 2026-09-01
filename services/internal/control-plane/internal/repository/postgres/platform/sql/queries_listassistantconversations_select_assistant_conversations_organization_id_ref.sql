-- name: queries_listassistantconversations_select_assistant_conversations_organization_id_ref :many
SELECT c.ref,c.title,c.title_source,c.title_revision,COALESCE(p.ref,''),s.ref,c.state,c.version,
       c.context_route,c.context_entity_kind,c.context_entity_ref,c.context_entity_name,
       c.context_entity_version,c.allowed_operations,c.created_at,c.updated_at
FROM control_plane.assistant_conversations c
LEFT JOIN control_plane.projects p ON p.id=c.project_id
JOIN control_plane.sessions s ON s.id=c.session_id
WHERE c.organization_id=$1::uuid AND ($2='' OR p.ref=$2)
ORDER BY c.updated_at DESC LIMIT $3
