-- name: catalog_cursor__upsert :one
-- params: @arg1,@arg2,@arg3,@arg4,@arg5,@arg6,@arg7
INSERT INTO interaction_gateway_agent_bot_catalog_cursors(
    cursor_id, organization_id, project_id, actor_id, catalog_offset, page_size, expires_at
) VALUES (@arg1::uuid, @arg2::uuid, @arg3::uuid, @arg4::uuid, @arg5::integer, @arg6::integer,
          clock_timestamp() + @arg7::interval)
ON CONFLICT (cursor_id) DO UPDATE SET expires_at = EXCLUDED.expires_at
WHERE interaction_gateway_agent_bot_catalog_cursors.organization_id = EXCLUDED.organization_id
  AND interaction_gateway_agent_bot_catalog_cursors.project_id = EXCLUDED.project_id
  AND interaction_gateway_agent_bot_catalog_cursors.actor_id = EXCLUDED.actor_id
  AND interaction_gateway_agent_bot_catalog_cursors.catalog_offset = EXCLUDED.catalog_offset
  AND interaction_gateway_agent_bot_catalog_cursors.page_size = EXCLUDED.page_size
RETURNING cursor_id::text;
