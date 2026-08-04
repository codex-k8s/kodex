INSERT INTO interaction_gateway_inbound_events
    (id, provider_event_id, kind, revision, payload, digest_sha256, state,
     organization_id, project_id, attempts, processing_expires_at, next_attempt_at)
VALUES ($1, $2, $3, $4, $5, $6, 'PROCESSING', $7, $8, 1, now() + $9::interval, now())
ON CONFLICT (provider_event_id) DO NOTHING;
