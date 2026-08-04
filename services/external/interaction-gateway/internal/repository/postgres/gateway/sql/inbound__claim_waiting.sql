WITH candidate AS (
    SELECT id
    FROM interaction_gateway_inbound_events
    WHERE ((state IN ('PENDING', 'WAITING_SCAN', 'WAITING_CLEANUP') AND next_attempt_at <= clock_timestamp()) OR
           (state = 'PROCESSING' AND processing_expires_at <= clock_timestamp()))
    ORDER BY next_attempt_at, created_at
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
UPDATE interaction_gateway_inbound_events AS inbound
SET state = 'PROCESSING',
    attempts = LEAST(inbound.attempts + CASE WHEN inbound.state = 'WAITING_SCAN' THEN 0 ELSE 1 END, 32),
    scan_polls = inbound.scan_polls + CASE WHEN inbound.state = 'WAITING_SCAN' THEN 1 ELSE 0 END,
    fence = inbound.fence + 1, lease_owner = $2, lease_token_sha256 = $3,
    processing_expires_at = clock_timestamp() + $1::interval, updated_at = clock_timestamp()
FROM candidate
WHERE inbound.id = candidate.id
RETURNING inbound.id, inbound.provider_event_id, inbound.kind, inbound.revision,
          inbound.payload, inbound.digest_sha256, inbound.state,
          inbound.organization_id, inbound.project_id, inbound.session_id,
          inbound.prompt_artifact_id, inbound.attachment_artifacts,
          inbound.attempts, inbound.scan_polls, inbound.fence, inbound.next_attempt_at,
          inbound.created_at, inbound.updated_at, inbound.processing_expires_at,
          true, inbound.semantic_outcome, inbound.response_message,
          inbound.terminal_error_code, inbound.next_action;
