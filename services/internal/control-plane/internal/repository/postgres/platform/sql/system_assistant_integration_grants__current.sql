-- name: system_assistant_integration_grants__current :many
SELECT grant_row.ref, grant_row.version, grant_row.capability_key, grant_row.approval_policy,
       grant_row.approval_scope_paths,grant_row.enabled
FROM control_plane.integration_grants grant_row
JOIN control_plane.integration_connections connection ON connection.id=grant_row.connection_id
 AND connection.organization_id=grant_row.organization_id
WHERE grant_row.organization_id=$1::uuid AND connection.ref=$2
 AND grant_row.target_kind='AGENT' AND grant_row.target_ref=$3
ORDER BY grant_row.capability_key;
