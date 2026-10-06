-- name: assistant_conversation_context :one
SELECT COALESCE(project.ref,''), conversation.context_route, conversation.context_entity_kind,
       conversation.context_entity_ref, projection.entity_name, projection.entity_version,
       projection.allowed_operations || CASE
           WHEN control_plane.assistant_project_profile_creation_allowed(conversation.organization_id,
               @actor_id::uuid,conversation.project_id,conversation.assistant_scope,conversation.context_entity_kind)
               THEN ARRAY['CREATE_PROJECT_ASSISTANT']::text[] ELSE '{}'::text[] END
       || control_plane.assistant_system_integration_grant_operations(conversation.organization_id,
           @actor_id::uuid,conversation.assistant_agent_id,conversation.assistant_scope,NULLIF(@authority_project,'')::uuid)
       || control_plane.assistant_project_connection_operations(conversation.organization_id,
           @actor_id::uuid,conversation.assistant_agent_id,conversation.assistant_scope,NULLIF(@authority_project,'')::uuid)
       || control_plane.assistant_project_integration_grant_operations(conversation.organization_id,
           @actor_id::uuid,conversation.assistant_agent_id,conversation.assistant_scope,NULLIF(@authority_project,'')::uuid)
FROM control_plane.assistant_conversations conversation
LEFT JOIN control_plane.projects project ON project.id=conversation.project_id
JOIN LATERAL control_plane.assistant_context_projection_v2(conversation.organization_id,@actor_id::uuid,
    NULLIF(@authority_project,'')::uuid,conversation.context_entity_kind,conversation.context_entity_ref,transaction_timestamp(),conversation.project_id) projection ON true
WHERE conversation.organization_id=@organization_id::uuid AND conversation.created_by=@actor_id::uuid
  AND ((@conversation_ref<>'' AND conversation.ref=@conversation_ref)
       OR (@plan_ref<>'' AND EXISTS (SELECT 1 FROM control_plane.assistant_plans plan
           WHERE plan.organization_id=conversation.organization_id AND plan.conversation_ref=conversation.ref AND plan.ref=@plan_ref)))
  AND (@authority_project='' OR conversation.project_id IS NULL OR conversation.project_id=NULLIF(@authority_project,'')::uuid)
  AND (conversation.project_id IS NULL OR (project.lifecycle='ACTIVE' AND control_plane.catalog_resource_visible(
      conversation.organization_id,@actor_id::uuid,'project.view','PROJECT',project.id,project.id,project.created_by,'{}'::jsonb,transaction_timestamp())));
