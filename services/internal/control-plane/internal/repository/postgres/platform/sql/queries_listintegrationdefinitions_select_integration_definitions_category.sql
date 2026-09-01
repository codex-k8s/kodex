-- name: queries_listintegrationdefinitions_select_integration_definitions_category :many
SELECT stable_key,name,description,category,optional,enabled,capabilities,configuration_schema,
	schema_version,definition_version,origin,digest,adapter,COALESCE(credential_secret_key,'')
FROM control_plane.integration_definitions
WHERE ($1 = '' OR category = $1)
  AND ($2 = '' OR stable_key ILIKE '%' || $2 || '%' OR name ILIKE '%' || $2 || '%' OR description ILIKE '%' || $2 || '%')
  AND ($3 = '' OR stable_key > $3)
ORDER BY stable_key
LIMIT $4
