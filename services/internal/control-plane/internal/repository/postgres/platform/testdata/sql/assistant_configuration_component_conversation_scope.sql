-- name: assistant_configuration_component_conversation_scope :one
SELECT COALESCE(project.ref,''),
       (SELECT COALESCE(convert_from(event.payload,'UTF8')::jsonb->>'projectRef','')
        FROM control_plane.outbox_events event
        WHERE convert_from(event.payload,'UTF8')::jsonb->>'organizationRef'=@organization_ref
          AND convert_from(event.payload,'UTF8')::jsonb->>'aggregateRef'=@plan_ref
          AND convert_from(event.payload,'UTF8')::jsonb->>'eventName'='SYSTEM_ASSISTANT_CHANGED'
        ORDER BY event.sequence DESC LIMIT 1)
FROM control_plane.assistant_conversations conversation
LEFT JOIN control_plane.projects project ON project.id=conversation.project_id
WHERE conversation.organization_id=@organization_id::uuid AND conversation.ref=@conversation_ref;
