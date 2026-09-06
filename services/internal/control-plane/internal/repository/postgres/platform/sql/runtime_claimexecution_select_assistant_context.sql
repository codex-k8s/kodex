-- name: runtime_claimexecution_select_assistant_context :one
SELECT CASE WHEN EXISTS (SELECT 1 FROM control_plane.assistant_conversations conversation
    WHERE conversation.organization_id=$1::uuid AND conversation.session_id=$2::uuid) THEN (
    SELECT jsonb_build_object(
        'route',conversation.context_route,
        'entityKind',conversation.context_entity_kind,
        'entityRef',conversation.context_entity_ref,
        'entityName',context.entity_name,
        'entityVersion',context.entity_version,
        'allowedOperations',context.allowed_operations
    )
    FROM control_plane.assistant_conversations conversation
    JOIN LATERAL control_plane.assistant_context_projection(conversation.organization_id,conversation.created_by,
        conversation.project_id,conversation.context_entity_kind,conversation.context_entity_ref,transaction_timestamp(),conversation.project_id) context ON true
    LEFT JOIN control_plane.projects project ON project.id=conversation.project_id
    WHERE conversation.organization_id=$1::uuid AND conversation.session_id=$2::uuid
      AND (conversation.project_id IS NULL OR (project.lifecycle='ACTIVE' AND control_plane.catalog_resource_visible(
          conversation.organization_id,conversation.created_by,'project.view','PROJECT',project.id,project.id,project.created_by,'{}'::jsonb,transaction_timestamp())))
) ELSE '{}'::jsonb END
