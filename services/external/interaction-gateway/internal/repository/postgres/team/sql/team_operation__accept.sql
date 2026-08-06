UPDATE interaction_gateway_team_operations
SET state = 'PROVIDER_ACCEPTED',
    selector_id = $2::uuid,
    provider_team_id = $3::text,
    provider_status = $4::text,
    provider_snapshot_sha256 = $5::text,
    provider_receipt_sha256 = $6::text,
    provider_generation = $7::bigint,
    provider_created_at = $8::timestamptz,
    provider_updated_at = $9::timestamptz,
    provider_observed_at = $10::timestamptz,
    failure_code = '',
    lease_owner = '', lease_token_sha256 = '', lease_expires_at = NULL,
    updated_at = clock_timestamp()
WHERE operation_id = $1::uuid
  AND state IN ('EFFECT_PENDING', 'AMBIGUOUS')
  AND fence = $11::bigint
  AND lease_token_sha256 = $12::text;
