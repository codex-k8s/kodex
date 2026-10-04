-- name: assistant_configuration_catalog__assistants :many
SELECT agent.ref, agent.name, agent.version,
       CASE WHEN agent.system_key='system-assistant' THEN 'ORGANIZATION' ELSE 'PROJECT' END,
       COALESCE(project.ref,''), COALESCE(profile.ref,''), COALESCE(environment.ref,'')
FROM control_plane.agents agent
LEFT JOIN control_plane.assistant_runtime runtime ON runtime.agent_id=agent.id AND runtime.organization_id=agent.organization_id
LEFT JOIN control_plane.project_assistant_profiles profile
  ON profile.agent_id=agent.id AND profile.organization_id=agent.organization_id AND profile.project_id=agent.project_id
LEFT JOIN control_plane.projects project ON project.id=agent.project_id AND project.organization_id=agent.organization_id
LEFT JOIN control_plane.agent_runtime_environment_bindings binding ON binding.agent_id=agent.id AND binding.organization_id=agent.organization_id
LEFT JOIN control_plane.runtime_environment_sets environment ON environment.id=binding.environment_set_id AND environment.organization_id=agent.organization_id
  AND ((agent.project_id IS NULL AND environment.scope_kind='ORGANIZATION' AND environment.project_id IS NULL)
    OR (agent.project_id IS NOT NULL AND environment.scope_kind='PROJECT' AND environment.project_id=agent.project_id))
WHERE agent.organization_id=@organization_id::uuid AND agent.state<>'ARCHIVED'
 AND (@source_scope='SYSTEM' OR agent.ref=@source_ref)
 AND (@query='' OR strpos(lower(agent.name),lower(@query))>0 OR strpos(lower(agent.ref),lower(@query))>0)
 AND ((agent.system_key='system-assistant' AND agent.project_id IS NULL AND runtime.agent_id IS NOT NULL
       AND @authority_project='' AND control_plane.organization_role_image_actor_allowed(agent.organization_id,@actor_id::uuid))
   OR (agent.system_key IS NULL AND profile.id IS NOT NULL AND project.lifecycle='ACTIVE'
       AND (@authority_project='' OR agent.project_id=NULLIF(@authority_project,'')::uuid)
       AND control_plane.catalog_resource_visible(agent.organization_id,@actor_id::uuid,'agent.manage','AGENT',agent.id,agent.project_id,agent.created_by,jsonb_build_object('PROJECT',agent.project_id::text),transaction_timestamp())))
ORDER BY agent.name,agent.ref LIMIT 11 OFFSET @offset;
