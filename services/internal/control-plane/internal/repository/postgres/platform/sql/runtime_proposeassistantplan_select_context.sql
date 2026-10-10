-- name: runtime_proposeassistantplan_select_context :one
SELECT conversation.id::text,
       conversation.ref,
       conversation.version,
       COALESCE(conversation.project_id::text, ''),
       COALESCE(project.ref, ''),
       context.allowed_operations || CASE
           WHEN control_plane.assistant_project_profile_creation_allowed(run.organization_id,
               run.initiated_by,conversation.project_id,conversation.assistant_scope,run.assistant_context_entity_kind)
               THEN ARRAY['CREATE_PROJECT_ASSISTANT']::text[] ELSE '{}'::text[] END
       || control_plane.assistant_system_integration_grant_operations(run.organization_id,
           run.initiated_by,conversation.assistant_agent_id,conversation.assistant_scope,NULL::uuid)
       || control_plane.assistant_project_connection_operations(run.organization_id,
           run.initiated_by,conversation.assistant_agent_id,conversation.assistant_scope,run.project_id)
       || control_plane.assistant_project_integration_grant_operations(run.organization_id,
           run.initiated_by,conversation.assistant_agent_id,conversation.assistant_scope,run.project_id),
       run.assistant_context_entity_kind,
       run.assistant_context_entity_ref,
       run.target_ref,
       actor.id::text,
       actor.ref,
       actor.display_name,
       COALESCE(global_membership.role, 'MEMBER'),
       organization.ref
FROM control_plane.runs run
JOIN control_plane.assistant_conversations conversation
  ON conversation.organization_id = run.organization_id
 AND conversation.session_id = run.session_id
 AND conversation.state = 'ACTIVE'
 AND conversation.created_by = run.initiated_by
JOIN control_plane.agents assistant
  ON assistant.id=conversation.assistant_agent_id AND assistant.organization_id=run.organization_id
 AND assistant.ref=run.target_ref
JOIN control_plane.sessions session
  ON session.id=conversation.session_id AND session.organization_id=run.organization_id
 AND session.created_by=conversation.created_by AND session.target_ref=assistant.ref
LEFT JOIN control_plane.project_assistant_profiles profile
  ON profile.id=conversation.assistant_profile_id AND profile.organization_id=conversation.organization_id
 AND profile.project_id=conversation.project_id AND profile.agent_id=assistant.id
JOIN control_plane.subjects actor
  ON actor.organization_id = run.organization_id
 AND actor.id = run.initiated_by
 AND actor.active
JOIN control_plane.organizations organization ON organization.id = run.organization_id
JOIN LATERAL control_plane.assistant_context_projection_v3(run.organization_id,actor.id,run.project_id,
    run.assistant_context_entity_kind,run.assistant_context_entity_ref,transaction_timestamp(),conversation.project_id,conversation.assistant_scope) context ON true
LEFT JOIN control_plane.projects project ON project.id = conversation.project_id
LEFT JOIN LATERAL (
    SELECT membership.role
    FROM control_plane.memberships membership
    WHERE membership.organization_id = run.organization_id
      AND membership.subject_id = actor.id
      AND membership.project_id IS NULL
      AND membership.active
    LIMIT 1
) global_membership ON true
WHERE run.organization_id = $1::uuid
  AND run.id = $2::uuid
  AND run.target_type = 'SYSTEM_ASSISTANT'
  AND ((conversation.assistant_scope='SYSTEM' AND assistant.system_key='system-assistant' AND assistant.project_id IS NULL)
    OR (conversation.assistant_scope='PROJECT' AND assistant.system_key IS NULL AND profile.id IS NOT NULL))
  AND (conversation.project_id IS NULL OR (project.lifecycle='ACTIVE' AND control_plane.catalog_resource_visible(
      run.organization_id,actor.id,'project.view','PROJECT',project.id,project.id,project.created_by,'{}'::jsonb,transaction_timestamp())))
  AND EXISTS (
      SELECT 1
      FROM control_plane.memberships membership
      WHERE membership.organization_id = run.organization_id
        AND membership.subject_id = actor.id
        AND membership.active
  )
FOR UPDATE OF conversation;
