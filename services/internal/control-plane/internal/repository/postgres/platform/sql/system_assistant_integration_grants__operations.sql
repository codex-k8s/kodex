-- name: system_assistant_integration_grants__operations :one
SELECT control_plane.assistant_system_integration_grant_operations(
    @organization_id::uuid,@actor_id::uuid,@assistant_agent_id::uuid,@assistant_scope,
    NULLIF(@authority_project,'')::uuid)
