INSERT INTO interaction_gateway_inbound_events
    (id, provider_event_id, kind, revision, payload, digest_sha256, state,
     organization_id, project_id, attempts, fence, lease_owner, lease_token_sha256,
     processing_expires_at, next_attempt_at)
VALUES ($1, $2, $3, $4, $5, $6, 'PROCESSING', $7, $8, 1, 1, $10, $11,
        clock_timestamp() + $9::interval, clock_timestamp())
ON CONFLICT (provider_event_id) DO NOTHING;
