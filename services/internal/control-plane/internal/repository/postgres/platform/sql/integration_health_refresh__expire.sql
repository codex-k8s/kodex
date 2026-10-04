-- name: integration_health_refresh__expire :exec
WITH expired AS (
SELECT id FROM control_plane.integration_connection_tests
WHERE organization_id=@organization_id::uuid AND purpose='MANAGED_MCP_REFRESH'
  AND state='CLAIMED' AND claimed_workload='integration-gateway' AND lease_expires_at<=clock_timestamp()
ORDER BY lease_expires_at,ref FOR UPDATE SKIP LOCKED LIMIT @limit
)
UPDATE control_plane.integration_connection_tests t
SET state='FAILED',lease_ref=NULL,fence_digest=NULL,workload_instance=NULL,lease_expires_at=NULL,
    safe_error_code='INTEGRATION_UNAVAILABLE',completed_at=clock_timestamp(),retry_after=clock_timestamp()+INTERVAL '30 seconds',version=t.version+1,updated_at=clock_timestamp()
FROM expired WHERE t.id=expired.id;
