INSERT INTO interaction_gateway_team_catalog_selectors(
    selector_id, organization_id, project_id, actor_id, provider_team_id,
    display_name, slug, status, provider_snapshot_sha256,
    provider_created_at, provider_updated_at, observed_at, expires_at
) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::text,
          $6::text, $7::text, $8::text, $9::text,
          $10::timestamptz, $11::timestamptz, $12::timestamptz,
          clock_timestamp() + $13::interval)
ON CONFLICT (organization_id, project_id, actor_id, provider_team_id) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    slug = EXCLUDED.slug,
    status = EXCLUDED.status,
    provider_snapshot_sha256 = EXCLUDED.provider_snapshot_sha256,
    provider_created_at = EXCLUDED.provider_created_at,
    provider_updated_at = EXCLUDED.provider_updated_at,
    observed_at = EXCLUDED.observed_at,
    expires_at = EXCLUDED.expires_at,
    updated_at = clock_timestamp()
RETURNING selector_id::text;
