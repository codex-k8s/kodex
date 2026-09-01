-- name: runtime_claimexecution_select_assistant_context :one
SELECT COALESCE((
    SELECT jsonb_build_object(
        'route',conversation.context_route,
        'entityKind',conversation.context_entity_kind,
        'entityRef',conversation.context_entity_ref,
        'entityName',conversation.context_entity_name,
        'entityVersion',conversation.context_entity_version,
        'allowedOperations',conversation.allowed_operations
    )
    FROM control_plane.assistant_conversations conversation
    WHERE conversation.organization_id=$1::uuid AND conversation.session_id=$2::uuid
), '{}'::jsonb)
