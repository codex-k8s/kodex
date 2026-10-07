-- name: system_assistant_integration_grants__snapshot :one
SELECT COALESCE(grant_row.ref,''),COALESCE(grant_row.version,0),COALESCE(grant_row.approval_policy,''),
       COALESCE(grant_row.enabled,false),COALESCE(grant_row.approval_scope_paths,'{}'::text[])
FROM control_plane.integration_connections connection
LEFT JOIN control_plane.integration_grants grant_row ON grant_row.connection_id=connection.id
 AND grant_row.organization_id=connection.organization_id AND grant_row.target_kind='AGENT'
 AND grant_row.target_ref=$3 AND grant_row.capability_key=$4
WHERE connection.organization_id=$1::uuid AND connection.ref=$2;
