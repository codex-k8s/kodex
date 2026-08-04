-- name: SessionRelease
UPDATE integration_gateway.transport_sessions SET
    concurrent_requests = GREATEST(concurrent_requests - 1, 0)
 WHERE transport_session_id = @transport_session_id
   AND tenant_id = @tenant_id AND project_id = @project_id
RETURNING transport_session_id
