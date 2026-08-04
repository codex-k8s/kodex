UPDATE interaction_gateway_turn_watches
SET last_version = GREATEST(last_version, $4),
    state = CASE WHEN $5::boolean THEN 'TERMINAL' ELSE 'ACTIVE' END,
    next_poll_at = $6, lease_owner = '', lease_token_sha256 = '', lease_expires_at = NULL,
    updated_at = clock_timestamp()
WHERE turn_id = $1 AND fence = $2 AND lease_token_sha256 = $3 AND state = 'ACTIVE';
