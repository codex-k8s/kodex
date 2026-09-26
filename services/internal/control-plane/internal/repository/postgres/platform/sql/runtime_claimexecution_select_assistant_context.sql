-- name: runtime_claimexecution_select_assistant_context :one
SELECT CASE WHEN EXISTS (SELECT 1 FROM control_plane.runs candidate
    WHERE candidate.organization_id=$1::uuid AND candidate.id=$2::uuid AND candidate.target_type='SYSTEM_ASSISTANT') THEN (
    SELECT jsonb_build_object(
        'route',run.assistant_context_route,
        'entityKind',run.assistant_context_entity_kind,
        'entityRef',run.assistant_context_entity_ref,
        'entityName',context.entity_name,
        'entityVersion',context.entity_version,
        'allowedOperations',context.allowed_operations
    )
    FROM control_plane.runs run
    JOIN control_plane.assistant_conversations conversation
      ON conversation.organization_id=run.organization_id AND conversation.session_id=run.session_id
    JOIN LATERAL control_plane.assistant_context_projection(run.organization_id,run.initiated_by,
        run.project_id,run.assistant_context_entity_kind,run.assistant_context_entity_ref,transaction_timestamp(),conversation.project_id) context ON true
    LEFT JOIN control_plane.projects project ON project.id=conversation.project_id
    WHERE run.organization_id=$1::uuid AND run.id=$2::uuid AND run.target_type='SYSTEM_ASSISTANT'
      AND conversation.state='ACTIVE'
      AND (conversation.project_id IS NULL OR (project.lifecycle='ACTIVE' AND control_plane.catalog_resource_visible(
          run.organization_id,run.initiated_by,'project.view','PROJECT',project.id,project.id,project.created_by,'{}'::jsonb,transaction_timestamp())))
) ELSE '{}'::jsonb END
