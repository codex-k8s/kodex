-- name: platform__queries_listintegrationconnections_1 :many
SELECT c.ref,c.definition_key,d.name,c.name,c.state,c.masked_credentials_state,c.last_test_summary,c.enabled,c.version,c.public_configuration,d.capabilities,c.last_tested_at,c.created_at,c.updated_at
FROM control_plane.integration_connections c
JOIN control_plane.integration_definitions d ON d.stable_key=c.definition_key
WHERE c.organization_id=$1::uuid AND ($2='' OR c.definition_key=$2)
ORDER BY c.updated_at DESC
LIMIT $3
