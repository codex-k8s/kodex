-- name: effective_capabilities_grant_target :one
SELECT connection.ref
FROM control_plane.integration_grants grant_row
JOIN control_plane.integration_connections connection ON connection.id=grant_row.connection_id
 AND connection.organization_id=grant_row.organization_id
WHERE grant_row.organization_id=$1::uuid AND grant_row.ref=$2
 AND grant_row.target_kind='AGENT' AND grant_row.target_ref=$3
 AND connection.lifecycle_state='ACTIVE';
