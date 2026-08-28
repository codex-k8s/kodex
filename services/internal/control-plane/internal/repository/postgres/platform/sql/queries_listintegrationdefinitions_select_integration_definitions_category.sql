-- name: queries_listintegrationdefinitions_select_integration_definitions_category :many
SELECT stable_key,name,description,category,optional,enabled,capabilities,configuration_schema,
	schema_version,definition_version,origin,digest,adapter,COALESCE(credential_secret_key,'')
FROM control_plane.integration_definitions
WHERE ($1='' OR category=$1)
ORDER BY category,name
