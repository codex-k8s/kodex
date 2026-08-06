SELECT provider_team_id
FROM interaction_gateway_team_catalog_selectors
WHERE selector_id = $1::uuid
  AND organization_id = $2::uuid
  AND project_id = $3::uuid
  AND actor_id = $4::uuid
  AND expires_at > clock_timestamp();
