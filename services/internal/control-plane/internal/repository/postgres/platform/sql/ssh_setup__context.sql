-- name: ssh_setup__context :one
SELECT connection.public_configuration,clock_timestamp()
FROM control_plane.integration_connections connection
JOIN control_plane.subjects actor ON actor.organization_id=connection.organization_id
 AND actor.id=@actor_id::uuid AND actor.kind='USER' AND actor.active
WHERE connection.organization_id=@organization_id::uuid AND connection.id=@connection_id::uuid
 AND connection.lifecycle_state='ACTIVE';
