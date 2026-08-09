-- name: catalog_cursor__resolve :one
-- params: @arg1,@arg2,@arg3,@arg4,@arg5
SELECT catalog_offset
FROM interaction_gateway_agent_bot_catalog_cursors
WHERE cursor_id = @arg1::uuid AND organization_id = @arg2::uuid AND project_id = @arg3::uuid
  AND actor_id = @arg4::uuid AND page_size = @arg5::integer AND expires_at > clock_timestamp();
