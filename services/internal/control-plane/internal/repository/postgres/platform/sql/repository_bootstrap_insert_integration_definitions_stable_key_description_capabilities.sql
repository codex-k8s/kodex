-- name: repository_bootstrap_insert_integration_definitions_stable_key_description_capabilities :exec
INSERT INTO control_plane.integration_definitions
	(stable_key,name,description,category,capabilities,configuration_schema,schema_version,definition_version,origin,digest,adapter,credential_secret_key)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NULLIF($12,''))
ON CONFLICT (stable_key) DO UPDATE SET
	name=EXCLUDED.name,
	description=EXCLUDED.description,
	category=EXCLUDED.category,
	capabilities=EXCLUDED.capabilities,
	configuration_schema=EXCLUDED.configuration_schema,
	schema_version=EXCLUDED.schema_version,
	definition_version=EXCLUDED.definition_version,
	origin=EXCLUDED.origin,
	digest=EXCLUDED.digest,
	adapter=EXCLUDED.adapter,
	credential_secret_key=EXCLUDED.credential_secret_key,
	enabled=true,
	version=control_plane.integration_definitions.version+1
