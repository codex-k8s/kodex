-- name: SessionClose
UPDATE integration_gateway.transport_sessions SET
    status = 'CLOSED', expires_at = LEAST(expires_at, @closed_at),
    concurrent_requests = 0, last_seen_at = @closed_at
 WHERE transport_session_id = @transport_session_id
   AND status IN ('INITIALIZING', 'ACTIVE')
RETURNING transport_session_id
