-- name: queries_getintegrationconnection_select_integration_connections_organization_id_ref :one
SELECT c.ref,c.definition_key,d.name,c.name,c.state,c.masked_credentials_state,c.last_test_summary,c.enabled,c.version,c.public_configuration,d.capabilities,c.last_tested_at,c.created_at,c.updated_at
FROM control_plane.integration_connections c
JOIN control_plane.integration_definitions d ON d.stable_key=c.definition_key
WHERE c.organization_id=$1::uuid AND c.ref=$2
