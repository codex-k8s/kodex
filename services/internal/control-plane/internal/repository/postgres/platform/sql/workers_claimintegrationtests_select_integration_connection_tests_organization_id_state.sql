-- name: workers_claimintegrationtests_select_integration_connection_tests_organization_id_state :many
SELECT t.id::text,t.ref,t.generation,c.ref,c.definition_key,c.public_configuration,
	c.definition_version,c.definition_digest,
	COALESCE(cr.ref,''),COALESCE(cr.revision,0),COALESCE(cr.secret_ref,''),COALESCE(cr.secret_uid::text,''),
	COALESCE(cr.secret_resource_version,''),COALESCE(cr.content_sha256,''),cr.created_at
FROM control_plane.integration_connection_tests t
JOIN control_plane.integration_connections c ON c.id=t.connection_id
JOIN control_plane.integration_definitions d ON d.stable_key=c.definition_key
LEFT JOIN control_plane.integration_credential_revisions cr ON cr.id=c.credential_revision_id
WHERE t.organization_id=$1::uuid AND t.state='DUE' AND c.enabled
  AND ((t.purpose='OWNER_TEST' AND c.state='TESTING') OR
       (t.purpose='MANAGED_MCP_REFRESH' AND $3='integration-gateway'
        AND control_plane.context7_health_refresh_eligible(t.organization_id,c.id)))
  AND t.input_snapshot <> '{}'::jsonb
  AND t.input_snapshot->>'connectionRef'=c.ref
  AND (t.input_snapshot->>'connectionVersion')::bigint=c.version
  AND t.input_snapshot->'configuration'=c.public_configuration
  AND t.input_snapshot->>'definitionVersion'=c.definition_version
  AND t.input_snapshot->>'definitionDigest'=c.definition_digest
  AND t.input_snapshot->>'credentialRevisionRef'=COALESCE(cr.ref,'')
  AND t.input_snapshot->>'credentialSHA256'=COALESCE(cr.content_sha256,'')
  AND (t.purpose='MANAGED_MCP_REFRESH' OR t.attempt=1 OR t.updated_at<=clock_timestamp()-INTERVAL '5 seconds')
  AND d.enabled AND d.adapter_owner=$3 AND d.execution_route=$4 AND d.adapter_readiness='READY'
ORDER BY t.created_at
FOR UPDATE OF t SKIP LOCKED
LIMIT $2
