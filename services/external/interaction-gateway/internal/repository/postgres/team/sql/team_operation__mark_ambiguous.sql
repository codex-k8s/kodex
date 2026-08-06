UPDATE interaction_gateway_team_operations
SET state = 'AMBIGUOUS',
    failure_code = $2::text,
    retry_not_before = $3::timestamptz,
    lease_owner = '', lease_token_sha256 = '', lease_expires_at = NULL,
    updated_at = clock_timestamp()
WHERE operation_id = $1::uuid
  AND state IN ('EFFECT_PENDING', 'AMBIGUOUS')
  AND fence = $4::bigint
  AND lease_token_sha256 = $5::text;
