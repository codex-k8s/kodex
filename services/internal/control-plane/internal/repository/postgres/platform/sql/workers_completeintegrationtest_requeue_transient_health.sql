-- name: workers_completeintegrationtest_requeue_transient_health :one
UPDATE control_plane.integration_connection_tests t
SET state='DUE',attempt=attempt+1,lease_ref=NULL,fence_digest=NULL,
    workload_instance=NULL,lease_expires_at=NULL,version=version+1,
    updated_at=clock_timestamp()
WHERE id=$1::uuid AND state='CLAIMED' AND lease_ref=$2 AND generation=$3
  AND t.purpose='OWNER_TEST'
  AND EXISTS (
    SELECT 1 FROM control_plane.integration_connections c
    JOIN control_plane.integration_definitions d ON d.stable_key=c.definition_key
    LEFT JOIN control_plane.integration_credential_revisions cr ON cr.id=c.credential_revision_id
    WHERE c.id=t.connection_id AND c.organization_id=t.organization_id
      AND c.enabled AND c.state='TESTING' AND d.enabled
      AND d.adapter_owner='integration-gateway' AND d.execution_route='MANAGED_MCP' AND d.adapter_readiness='READY'
      AND t.input_snapshot->>'connectionRef'=c.ref
      AND (t.input_snapshot->>'connectionVersion')::bigint=c.version
      AND t.input_snapshot->'configuration'=c.public_configuration
      AND t.input_snapshot->>'definitionVersion'=c.definition_version
      AND t.input_snapshot->>'definitionDigest'=c.definition_digest
      AND t.input_snapshot->>'credentialRevisionRef'=COALESCE(cr.ref,'')
      AND t.input_snapshot->>'credentialSHA256'=COALESCE(cr.content_sha256,'')
  )
RETURNING ref
