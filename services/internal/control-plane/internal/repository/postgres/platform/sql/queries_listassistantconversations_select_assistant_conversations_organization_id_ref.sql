-- name: queries_listassistantconversations_select_assistant_conversations_organization_id_ref :many
SELECT c.ref,c.title,c.title_source,c.title_revision,COALESCE(p.ref,''),s.ref,c.state,c.version,
       c.context_route,c.context_entity_kind,c.context_entity_ref,context.entity_name,
       context.entity_version,context.allowed_operations || CASE
           WHEN control_plane.assistant_project_profile_creation_allowed(c.organization_id,
               @actor_id::uuid,c.project_id,c.assistant_scope,c.context_entity_kind)
               THEN ARRAY['CREATE_PROJECT_ASSISTANT']::text[] ELSE '{}'::text[] END,c.created_at,c.updated_at,
       c.assistant_scope,assistant.ref,COALESCE(profile.ref,'')
FROM control_plane.assistant_conversations c
LEFT JOIN control_plane.projects p ON p.id=c.project_id
JOIN control_plane.sessions s ON s.id=c.session_id
JOIN control_plane.agents assistant ON assistant.id=c.assistant_agent_id AND assistant.organization_id=c.organization_id
LEFT JOIN control_plane.project_assistant_profiles profile ON profile.id=c.assistant_profile_id AND profile.organization_id=c.organization_id
JOIN LATERAL control_plane.assistant_context_projection_v2(c.organization_id,@actor_id::uuid,
    NULLIF(@authority_project,'')::uuid,c.context_entity_kind,c.context_entity_ref,@evaluated_at,c.project_id) context ON true
WHERE c.organization_id=@organization_id::uuid AND c.created_by=@actor_id::uuid
  AND (@project_ref='' OR p.ref=@project_ref) AND c.state=@state
  AND (@assistant_scope='' OR c.assistant_scope=@assistant_scope)
  AND (@assistant_ref='' OR assistant.ref=@assistant_ref)
  AND (@authority_project='' OR c.project_id IS NULL OR c.project_id=NULLIF(@authority_project,'')::uuid)
  AND (@query='' OR strpos(lower(c.title || ' ' || c.ref),lower(@query))>0
    OR (@match_localized_default_title AND c.title='i18n:NEW_ASSISTANT_CONVERSATION'))
  AND ((p.id IS NULL AND control_plane.catalog_resource_visible(c.organization_id,@actor_id::uuid,
        'organization.view','ORGANIZATION',c.organization_id,NULL::uuid,NULL::uuid,'{}'::jsonb,@evaluated_at))
    OR (p.lifecycle='ACTIVE' AND control_plane.catalog_resource_visible(c.organization_id,@actor_id::uuid,
        'project.view','PROJECT',p.id,p.id,p.created_by,'{}'::jsonb,@evaluated_at)))
  AND (@cursor_ref='' OR (c.created_at,c.ref)<(@cursor_at::timestamptz,@cursor_ref))
ORDER BY c.created_at DESC,c.ref DESC LIMIT @page_size;
