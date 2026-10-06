-- name: project_assistant_connection__operations :one
SELECT control_plane.assistant_project_connection_operations(@organization_id::uuid,@actor_id::uuid,
    @assistant_agent_id::uuid,@assistant_scope,NULLIF(@authority_project,'')::uuid)
 || control_plane.assistant_project_integration_grant_operations(@organization_id::uuid,@actor_id::uuid,
    @assistant_agent_id::uuid,@assistant_scope,NULLIF(@authority_project,'')::uuid);
