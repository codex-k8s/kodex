-- name: configuration_applyassistantplancommand_promote_conversation_project :one
UPDATE control_plane.assistant_conversations conversation
SET project_id = project.id,
    context_route = '/projects/' || project.ref,
    context_entity_kind = 'PROJECT',
    context_entity_ref = project.ref,
    context_entity_name = projected.entity_name,
    context_entity_version = projected.entity_version,
    allowed_operations = projected.allowed_operations,
    version = conversation.version + 1,
    updated_at = clock_timestamp()
FROM control_plane.projects project
JOIN LATERAL control_plane.assistant_context_projection(
    $1::uuid, $2::uuid, project.id,
    'PROJECT', project.ref, transaction_timestamp(), project.id
) projected ON true
WHERE project.id = $3::uuid
  AND project.ref = $4
  AND project.organization_id = $1::uuid
  AND project.created_by = $2::uuid
  AND project.lifecycle = 'ACTIVE'
  AND conversation.ref = $5
  AND conversation.organization_id = $1::uuid
  AND conversation.created_by = $2::uuid
  AND conversation.project_id IS NULL
  AND conversation.state = 'ACTIVE'
  AND EXISTS (
    SELECT 1 FROM control_plane.sessions session
    WHERE session.id = conversation.session_id
      AND session.organization_id = conversation.organization_id
      AND session.created_by = conversation.created_by
      AND session.project_id = project.id
      AND session.state = 'ACTIVE'
  )
RETURNING conversation.ref;
