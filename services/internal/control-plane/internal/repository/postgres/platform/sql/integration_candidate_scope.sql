-- name: integration_candidate_scope :one
SELECT connection.public_configuration,COALESCE(grant_row.ref,''),COALESCE(grant_row.version,0)
FROM control_plane.integration_connections connection
LEFT JOIN control_plane.integration_grants grant_row ON grant_row.organization_id=connection.organization_id
 AND grant_row.connection_id=connection.id AND grant_row.target_kind=@recipient_kind
 AND grant_row.target_ref=@recipient_ref AND grant_row.capability_key=@capability_key
WHERE connection.organization_id=@organization_id::uuid AND connection.ref=@connection_ref
