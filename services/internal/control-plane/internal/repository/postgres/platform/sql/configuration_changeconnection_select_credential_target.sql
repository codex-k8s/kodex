-- name: configuration_changeconnection_select_credential_target :one
SELECT c.id::text,d.credential_secret_key
FROM control_plane.integration_connections c
JOIN control_plane.integration_definitions d ON d.stable_key=c.definition_key AND d.enabled
WHERE c.organization_id=$1::uuid AND c.ref=$2 AND c.version=$3 AND c.lifecycle_state='ACTIVE'
FOR UPDATE OF c
