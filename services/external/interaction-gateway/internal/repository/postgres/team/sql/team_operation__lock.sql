SELECT operation_id::text, organization_id::text, project_id::text, actor_id::text,
       idempotency_key::text, request_sha256, display_name, slug, state,
       COALESCE(selector_id::text, ''), provider_team_id, provider_status,
       provider_snapshot_sha256, provider_receipt_sha256, COALESCE(provider_generation, 0),
       failure_code, fence, COALESCE(effect_started_at, 'epoch'::timestamptz),
       retry_not_before, created_at, updated_at,
       COALESCE(provider_created_at, 'epoch'::timestamptz),
       COALESCE(provider_updated_at, 'epoch'::timestamptz),
       COALESCE(provider_observed_at, 'epoch'::timestamptz),
       lease_expires_at IS NOT NULL AND lease_expires_at > clock_timestamp()
FROM interaction_gateway_team_operations
WHERE operation_id = $1::uuid
FOR UPDATE;
