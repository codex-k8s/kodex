-- name: configuration_changeintegrationgrant_insert_integration_grants_ref_connection_id_target_kind :one
INSERT INTO control_plane.integration_grants(
	ref,organization_id,connection_id,capability_key,target_kind,target_ref,enabled,approval_policy,created_by,
	risk,resource_kind,resource_scope,resource_scope_digest,definition_version,definition_digest
)
VALUES($1,$2::uuid,$3::uuid,$4,$5,$6,true,$7,$8::uuid,$9,$10,$11,$12,$13,$14)
ON CONFLICT(connection_id,capability_key,target_kind,target_ref) DO UPDATE SET
	enabled=true,
	approval_policy=EXCLUDED.approval_policy,
	risk=EXCLUDED.risk,
	resource_kind=EXCLUDED.resource_kind,
	resource_scope=EXCLUDED.resource_scope,
	resource_scope_digest=EXCLUDED.resource_scope_digest,
	definition_version=EXCLUDED.definition_version,
	definition_digest=EXCLUDED.definition_digest,
	version=control_plane.integration_grants.version+1,
	updated_at=clock_timestamp()
RETURNING ref
