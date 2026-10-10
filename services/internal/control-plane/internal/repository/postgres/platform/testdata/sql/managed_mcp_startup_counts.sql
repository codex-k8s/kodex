SELECT
  (SELECT count(*) FROM control_plane.integration_connection_tests WHERE connection_id=c.id AND purpose='MANAGED_MCP_REFRESH'),
  (SELECT count(*) FROM control_plane.integration_connection_tests WHERE connection_id=c.id AND purpose='MANAGED_MCP_REFRESH' AND state='DUE'),
  (SELECT count(*) FROM control_plane.runtime_revisions WHERE run_id=r.id),
  (SELECT count(*) FROM control_plane.runtime_leases WHERE run_id=r.id AND state='CLAIMED'),
  (SELECT count(*) FROM control_plane.audit_events WHERE resource_ref=r.ref AND resource_kind='RUNTIME_CLAIM'),
  (SELECT count(*) FROM control_plane.idempotency_receipts WHERE idempotency_key=@idempotency_key),
  COALESCE((SELECT created_at::text FROM control_plane.integration_connection_tests WHERE connection_id=c.id AND purpose='MANAGED_MCP_REFRESH' AND state='DUE' LIMIT 1),'')
FROM control_plane.runs r
JOIN control_plane.integration_connections c ON c.organization_id=r.organization_id AND c.ref=@connection_ref
WHERE r.ref=@run_ref;
