WITH candidate AS (
    SELECT id
    FROM interaction_gateway_inbound_events
    WHERE state IN ('PENDING', 'WAITING_SCAN') AND next_attempt_at <= now() AND attempts < 32
    ORDER BY next_attempt_at, created_at
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
UPDATE interaction_gateway_inbound_events AS inbound
SET state = 'PROCESSING', attempts = attempts + 1,
    processing_expires_at = now() + $1::interval, updated_at = now()
FROM candidate
WHERE inbound.id = candidate.id
RETURNING inbound.id, inbound.provider_event_id, inbound.kind, inbound.revision,
          inbound.payload, inbound.digest_sha256, inbound.state,
          inbound.organization_id, inbound.project_id, inbound.session_id,
          inbound.prompt_artifact_id, inbound.attachment_artifacts,
          inbound.attempts, inbound.next_attempt_at, inbound.created_at, inbound.updated_at,
          inbound.processing_expires_at;
