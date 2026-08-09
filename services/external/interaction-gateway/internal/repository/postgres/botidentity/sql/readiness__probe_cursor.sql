-- name: readiness__probe_cursor :one
-- params: @arg1,@arg2,@arg3,@arg4
INSERT INTO interaction_gateway_agent_bot_catalog_cursors (
    cursor_id, organization_id, project_id, actor_id, catalog_offset, page_size, expires_at
) VALUES (@arg1::uuid, @arg2::uuid, @arg3::uuid, @arg4::uuid, 1, 1,
    clock_timestamp() + interval '1 minute')
ON CONFLICT (cursor_id) DO UPDATE SET created_at = interaction_gateway_agent_bot_catalog_cursors.created_at
RETURNING organization_id::text, project_id::text, actor_id::text, catalog_offset;
