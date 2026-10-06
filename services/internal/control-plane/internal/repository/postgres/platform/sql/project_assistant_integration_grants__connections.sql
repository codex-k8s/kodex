-- name: project_assistant_integration_grants__connections :many
SELECT connection.ref,connection.name
FROM control_plane.project_assistant_connection_purposes origin
JOIN control_plane.project_assistant_profiles profile ON profile.organization_id=origin.organization_id AND profile.ref=origin.profile_ref
JOIN control_plane.agents agent ON agent.id=profile.agent_id AND agent.organization_id=profile.organization_id AND agent.ref=origin.assistant_ref
JOIN control_plane.projects project ON project.id=profile.project_id AND project.organization_id=profile.organization_id AND project.ref=origin.project_ref
JOIN control_plane.integration_connections connection ON connection.id=origin.connection_id AND connection.organization_id=origin.organization_id
WHERE origin.organization_id=@organization_id::uuid AND agent.ref=@assistant_ref
  AND connection.lifecycle_state='ACTIVE'
  AND control_plane.catalog_resource_visible(origin.organization_id,@actor_id::uuid,'integration.manage','INTEGRATION',
      connection.id,NULL,connection.created_by,'{}'::jsonb,transaction_timestamp())
ORDER BY connection.ref LIMIT 101;
