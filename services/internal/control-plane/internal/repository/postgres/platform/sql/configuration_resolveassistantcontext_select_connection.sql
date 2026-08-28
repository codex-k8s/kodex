-- name: configuration_resolveassistantcontext_select_connection :one
SELECT name, version
FROM control_plane.integration_connections
WHERE organization_id=$1::uuid AND ref=$2
