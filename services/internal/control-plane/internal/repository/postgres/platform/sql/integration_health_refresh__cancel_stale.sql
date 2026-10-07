-- name: integration_health_refresh__cancel_stale :exec
WITH stale AS (
SELECT t.id FROM control_plane.integration_connection_tests t
JOIN control_plane.integration_connections c ON c.id=t.connection_id
LEFT JOIN control_plane.integration_credential_revisions cr ON cr.id=c.credential_revision_id
WHERE t.connection_id=c.id AND t.organization_id=@organization_id::uuid
  AND t.purpose='MANAGED_MCP_REFRESH' AND t.state IN ('DUE','CLAIMED')
  AND (NOT control_plane.context7_health_refresh_eligible(t.organization_id,c.id)
    OR t.input_snapshot->>'connectionVersion'<>c.version::text
    OR t.input_snapshot->'configuration'<>c.public_configuration
    OR t.input_snapshot->>'definitionVersion'<>c.definition_version
    OR t.input_snapshot->>'definitionDigest'<>c.definition_digest
    OR t.input_snapshot->>'credentialRevisionRef' IS DISTINCT FROM cr.ref
    OR t.input_snapshot->>'credentialRevision' IS DISTINCT FROM cr.revision::text
    OR t.input_snapshot->>'credentialSHA256' IS DISTINCT FROM cr.content_sha256)
ORDER BY t.created_at,t.ref FOR UPDATE OF t SKIP LOCKED LIMIT @limit
)
UPDATE control_plane.integration_connection_tests t
SET state='CANCELLED',lease_ref=NULL,fence_digest=NULL,workload_instance=NULL,lease_expires_at=NULL,
    safe_error_code='INTEGRATION_UNAVAILABLE',completed_at=clock_timestamp(),version=t.version+1,updated_at=clock_timestamp()
FROM stale WHERE t.id=stale.id;
