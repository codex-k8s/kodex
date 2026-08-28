-- name: configuration_changeconnection_insert_integration_connections_ref_definition_key_state :one
INSERT INTO control_plane.integration_connections(
	ref,organization_id,definition_key,name,state,enabled,credential_materialization_ref,
	masked_credentials_state,public_configuration,created_by,definition_version,definition_digest
)
SELECT $1,$2::uuid,d.stable_key,$3,'NOT_CONNECTED',true,NULL,$4,$5,$6::uuid,$7,$8
FROM control_plane.integration_definitions d
WHERE d.stable_key=$9 AND d.enabled AND d.definition_version=$7 AND d.digest=$8
RETURNING id::text,ref,definition_key,name,state,masked_credentials_state,enabled,version,public_configuration,created_at,updated_at
