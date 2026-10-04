-- name: integration_health_refresh__candidates :many
SELECT c.id::text,COALESCE(last_probe.ref,''),
       CASE WHEN last_probe.state='FAILED' AND last_probe.purpose='MANAGED_MCP_REFRESH'
            THEN last_probe.attempt+1 ELSE 1 END
FROM control_plane.integration_connections c
LEFT JOIN LATERAL (
  SELECT t.ref,t.state,t.purpose,t.attempt,t.completed_at,t.retry_after
  FROM control_plane.integration_connection_tests t
  WHERE t.connection_id=c.id AND t.completed_at IS NOT NULL
  ORDER BY t.completed_at DESC,t.ref DESC LIMIT 1
) last_probe ON true
WHERE c.organization_id=@organization_id::uuid
  AND control_plane.context7_health_refresh_eligible(c.organization_id,c.id)
  AND NOT EXISTS (SELECT 1 FROM control_plane.integration_connection_tests active
                  WHERE active.connection_id=c.id AND active.state IN ('DUE','CLAIMED'))
  AND ((last_probe.state='SUCCEEDED' AND last_probe.completed_at<=clock_timestamp()-INTERVAL '4 minutes')
       OR (last_probe.state='FAILED' AND last_probe.purpose='MANAGED_MCP_REFRESH'
           AND last_probe.attempt<3 AND last_probe.retry_after<=clock_timestamp())
       OR (last_probe.state='CANCELLED' AND last_probe.purpose='MANAGED_MCP_REFRESH' AND c.state='CONNECTED')
       OR last_probe.ref IS NULL)
ORDER BY c.ref
FOR UPDATE OF c SKIP LOCKED
LIMIT @limit;
