INSERT INTO interaction_gateway_team_operations(
    operation_id, organization_id, project_id, actor_id, kind,
    idempotency_key, request_sha256, display_name, slug, state,
    fence, lease_owner, lease_token_sha256, lease_expires_at, retry_not_before
) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'CREATE',
          $5::uuid, $6::text, $7::text, $8::text, 'PENDING',
          1, $9::text, $10::text, clock_timestamp() + $11::interval, clock_timestamp())
ON CONFLICT (organization_id, project_id, actor_id, kind, idempotency_key) DO NOTHING;
