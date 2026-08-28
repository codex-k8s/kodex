-- name: configuration_validateassistantplan_select_plan :one
SELECT p.id::text,p.conversation_ref,p.summary,p.operations,p.state,p.version,p.current_revision,
       p.content_digest,COALESCE(project.ref,'')
FROM control_plane.assistant_plans p
JOIN control_plane.assistant_conversations c ON c.ref=p.conversation_ref AND c.organization_id=p.organization_id
LEFT JOIN control_plane.projects project ON project.id=c.project_id
WHERE p.organization_id=$1::uuid AND p.ref=$2 AND p.state IN ('DRAFT','VALID','INVALID','STALE')
FOR UPDATE OF p
