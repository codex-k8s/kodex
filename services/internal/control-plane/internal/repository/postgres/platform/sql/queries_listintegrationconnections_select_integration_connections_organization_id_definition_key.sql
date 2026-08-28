-- name: queries_listintegrationconnections_select_integration_connections_organization_id_definition_key :many
SELECT c.ref,c.definition_key,d.name,c.name,c.state,c.masked_credentials_state,c.last_test_summary,c.enabled,c.version,
	c.public_configuration,d.capabilities,c.last_tested_at,c.created_at,c.updated_at,c.definition_version,c.definition_digest,
	COALESCE(d.credential_secret_key,''),
	COALESCE(cr.ref,''),COALESCE(cr.revision,0),COALESCE(cr.secret_ref,''),COALESCE(cr.secret_uid::text,''),
	COALESCE(cr.secret_resource_version,''),COALESCE(cr.content_sha256,''),cr.created_at
FROM control_plane.integration_connections c
JOIN control_plane.integration_definitions d ON d.stable_key=c.definition_key
LEFT JOIN control_plane.integration_credential_revisions cr ON cr.id=c.credential_revision_id
WHERE c.organization_id=$1::uuid AND ($2='' OR c.definition_key=$2)
ORDER BY c.updated_at DESC
LIMIT $3
