-- name: SessionTouch
WITH locked AS (
    SELECT * FROM integration_gateway.transport_sessions
     WHERE transport_session_id = @transport_session_id
       AND tenant_id = @tenant_id AND project_id = @project_id
     FOR UPDATE
), updated AS (
    UPDATE integration_gateway.transport_sessions AS session SET
        status = 'ACTIVE',
        request_count = session.request_count + 1,
        concurrent_requests = session.concurrent_requests + 1,
        expires_at = LEAST(@expires_at, session.created_at + interval '24 hours'),
        last_seen_at = @now
      FROM locked
     WHERE session.transport_session_id = locked.transport_session_id
       AND locked.token_digest = @token_digest
       AND locked.status IN ('INITIALIZING', 'ACTIVE')
       AND locked.expires_at > clock_timestamp()
       AND locked.request_count < @maximum_requests
       AND locked.concurrent_requests < @maximum_concurrency
    RETURNING session.payload, session.status, session.request_count,
              session.concurrent_requests, session.expires_at, session.last_seen_at,
              true AS acquired, true AS token_matches
)
SELECT * FROM updated
UNION ALL
SELECT locked.payload, locked.status, locked.request_count, locked.concurrent_requests,
       locked.expires_at, locked.last_seen_at, false AS acquired,
       locked.token_digest = @token_digest AS token_matches
  FROM locked WHERE NOT EXISTS (SELECT 1 FROM updated)
