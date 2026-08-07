-- name: team_selector__upsert :one
-- params: @arg1,@arg2,@arg3,@arg4,@arg5,@arg6,@arg7,@arg8,@arg9,@arg10,@arg11,@arg12,@arg13
INSERT INTO interaction_gateway_team_catalog_selectors(
    selector_id, organization_id, project_id, actor_id, provider_team_id,
    display_name, slug, status, provider_snapshot_sha256,
    provider_created_at, provider_updated_at, observed_at, expires_at
) VALUES (@arg1::uuid, @arg2::uuid, @arg3::uuid, @arg4::uuid, @arg5::text,
          @arg6::text, @arg7::text, @arg8::text, @arg9::text,
          @arg10::timestamptz, @arg11::timestamptz, @arg12::timestamptz,
          clock_timestamp() + @arg13::interval)
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
