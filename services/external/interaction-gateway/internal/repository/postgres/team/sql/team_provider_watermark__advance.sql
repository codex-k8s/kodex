INSERT INTO interaction_gateway_team_provider_watermarks(
    organization_id, project_id, provider_generation
) VALUES ($1::uuid, $2::uuid, 1)
ON CONFLICT (organization_id, project_id) DO UPDATE SET
    provider_generation = interaction_gateway_team_provider_watermarks.provider_generation + 1,
    updated_at = clock_timestamp()
RETURNING provider_generation;
