-- name: configuration_applyassistantplancommand_promote_session_project :one
UPDATE control_plane.sessions session
SET project_id = project.id,
    version = session.version + 1,
    updated_at = clock_timestamp()
FROM control_plane.assistant_conversations conversation
JOIN control_plane.projects project
  ON project.id = $1::uuid
 AND project.ref = $2
 AND project.organization_id = conversation.organization_id
 AND project.created_by = conversation.created_by
 AND project.lifecycle = 'ACTIVE'
WHERE session.id = conversation.session_id
  AND session.organization_id = $3::uuid
  AND session.created_by = $4::uuid
  AND session.project_id IS NULL
  AND session.state = 'ACTIVE'
  AND conversation.organization_id = $3::uuid
  AND conversation.created_by = $4::uuid
  AND conversation.ref = $5
  AND conversation.state = 'ACTIVE'
  AND conversation.project_id IS NULL
  AND NOT EXISTS (
    SELECT 1 FROM control_plane.runs run
    WHERE run.organization_id = session.organization_id
      AND run.session_id = session.id
      AND run.state NOT IN ('SUCCEEDED', 'FAILED', 'CANCELLED')
  )
RETURNING session.id::text;
