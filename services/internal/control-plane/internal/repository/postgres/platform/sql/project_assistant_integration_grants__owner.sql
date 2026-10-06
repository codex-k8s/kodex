-- name: project_assistant_integration_grants__owner :one
SELECT agent.ref, agent.name, agent.version, agent.enabled AND agent.state='READY',
       project.id::text, project.ref, profile.ref, profile.version
FROM control_plane.project_assistant_profiles profile
JOIN control_plane.projects project ON project.id=profile.project_id AND project.organization_id=profile.organization_id
JOIN control_plane.agents agent ON agent.id=profile.agent_id AND agent.organization_id=profile.organization_id
  AND agent.project_id=profile.project_id AND agent.system_key IS NULL
JOIN control_plane.project_assistant_connection_purposes origin ON origin.organization_id=profile.organization_id
  AND origin.project_ref=project.ref AND origin.profile_ref=profile.ref AND origin.assistant_ref=agent.ref
JOIN control_plane.integration_connections connection ON connection.id=origin.connection_id
  AND connection.organization_id=origin.organization_id AND connection.ref=@connection_ref
  AND connection.lifecycle_state='ACTIVE'
WHERE profile.organization_id=@organization_id::uuid AND agent.ref=@assistant_ref
  AND project.lifecycle='ACTIVE' AND agent.enabled AND agent.state<>'ARCHIVED'
  AND (@authority_project='' OR project.id=NULLIF(@authority_project,'')::uuid)
  AND control_plane.catalog_resource_visible(profile.organization_id,@actor_id::uuid,'project.manage','PROJECT',
      project.id,project.id,project.created_by,'{}'::jsonb,transaction_timestamp())
  AND control_plane.catalog_resource_visible(profile.organization_id,@actor_id::uuid,'agent.manage','AGENT',
      agent.id,project.id,agent.created_by,'{}'::jsonb,transaction_timestamp())
  AND control_plane.catalog_resource_visible(profile.organization_id,@actor_id::uuid,'integration.manage','INTEGRATION',
      connection.id,NULL,connection.created_by,'{}'::jsonb,transaction_timestamp())
FOR SHARE OF profile,project,agent,origin,connection;
