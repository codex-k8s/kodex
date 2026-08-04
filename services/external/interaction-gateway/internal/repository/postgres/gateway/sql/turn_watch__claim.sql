WITH candidate AS (
    SELECT turn_id
    FROM interaction_gateway_turn_watches
    WHERE state = 'ACTIVE' AND next_poll_at <= clock_timestamp()
      AND (lease_expires_at IS NULL OR lease_expires_at <= clock_timestamp())
    ORDER BY next_poll_at, created_at
    FOR UPDATE SKIP LOCKED
    LIMIT 1
), claimed AS (
    UPDATE interaction_gateway_turn_watches AS watch
    SET fence = watch.fence + 1, lease_owner = $1, lease_token_sha256 = $2,
        lease_expires_at = clock_timestamp() + $3::interval, updated_at = clock_timestamp()
    FROM candidate
    WHERE watch.turn_id = candidate.turn_id
    RETURNING watch.turn_id, watch.inbound_id, watch.last_version, watch.fence, watch.lease_expires_at
)
SELECT claimed.turn_id, claimed.last_version, claimed.fence, claimed.lease_expires_at,
       inbound.payload
FROM claimed
JOIN interaction_gateway_inbound_events AS inbound ON inbound.id = claimed.inbound_id;
