-- name: configuration_updateassistantplandraft_select_plan :one
SELECT p.id::text,p.conversation_ref,p.state,p.version,p.current_revision,COALESCE(project.ref,'')
FROM control_plane.assistant_plans p
JOIN control_plane.assistant_conversations c ON c.ref=p.conversation_ref AND c.organization_id=p.organization_id
LEFT JOIN control_plane.projects project ON project.id=c.project_id
WHERE p.organization_id=$1::uuid AND p.ref=$2
FOR UPDATE OF p
