-- name: workers_claimintegrationtests_select_integration_connection_tests_organization_id_state :many
SELECT t.id::text,t.ref,t.generation,c.ref,c.definition_key,c.public_configuration,
	c.definition_version,c.definition_digest,
	COALESCE(cr.ref,''),COALESCE(cr.revision,0),COALESCE(cr.secret_ref,''),COALESCE(cr.secret_uid::text,''),
	COALESCE(cr.secret_resource_version,''),COALESCE(cr.content_sha256,''),cr.created_at
FROM control_plane.integration_connection_tests t
JOIN control_plane.integration_connections c ON c.id=t.connection_id
JOIN control_plane.integration_definitions d ON d.stable_key=c.definition_key
LEFT JOIN control_plane.integration_credential_revisions cr ON cr.id=c.credential_revision_id
WHERE t.organization_id=$1::uuid AND t.state='DUE' AND c.enabled AND c.state='TESTING'
  AND d.enabled AND d.adapter_owner='integration-gateway' AND d.execution_route='MANAGED_MCP' AND d.adapter_readiness='READY'
ORDER BY t.created_at
FOR UPDATE OF t SKIP LOCKED
LIMIT $2
