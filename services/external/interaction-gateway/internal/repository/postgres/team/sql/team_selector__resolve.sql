-- name: team_selector__resolve :one
-- params: @arg1,@arg2,@arg3,@arg4
SELECT provider_team_id
FROM interaction_gateway_team_catalog_selectors
WHERE selector_id = @arg1::uuid
  AND organization_id = @arg2::uuid
  AND project_id = @arg3::uuid
  AND actor_id = @arg4::uuid
  AND expires_at > clock_timestamp();
