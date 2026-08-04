SELECT id, provider_event_id, kind, revision, payload, digest_sha256, state,
       organization_id, project_id, session_id, prompt_artifact_id,
       attachment_artifacts, attempts, scan_polls, fence, next_attempt_at,
       created_at, updated_at, processing_expires_at,
       processing_expires_at IS NOT NULL AND processing_expires_at > clock_timestamp(),
       semantic_outcome, response_message, terminal_error_code, next_action
FROM interaction_gateway_inbound_events
WHERE provider_event_id = $1
FOR UPDATE;
