INSERT INTO interaction_gateway_workspace_mapping_operations(
    operation_id, organization_id, project_id, actor_id, action, idempotency_key,
    request_sha256, mapping_id, expected_mapping_version, expected_mapping_generation,
    display_name, selector_id, provider_team_id, provider_status,
    provider_snapshot_sha256, provider_created_at, provider_updated_at, provider_observed_at,
    effect_generation, receipt_id, state, fence, lease_owner, lease_token_sha256, lease_expires_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::text, $6::uuid,
    $7::text, NULLIF($8::text, '')::uuid, $9::bigint, $10::bigint,
    $11::text, $12::uuid, $13::text, $14::text,
    $15::text, $16::timestamptz, $17::timestamptz, $18::timestamptz,
    $19::bigint, $20::uuid, 'PENDING', 1, $21::text, $22::text,
    clock_timestamp() + $23::interval)
ON CONFLICT (organization_id, project_id, actor_id, action, idempotency_key) DO NOTHING;
