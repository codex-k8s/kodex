SELECT id, provider_event_id, kind, revision, payload, digest_sha256, state,
       organization_id, project_id, session_id, prompt_artifact_id,
       attachment_artifacts, attempts, next_attempt_at, created_at, updated_at,
       processing_expires_at
FROM interaction_gateway_inbound_events
WHERE provider_event_id = $1
FOR UPDATE;
