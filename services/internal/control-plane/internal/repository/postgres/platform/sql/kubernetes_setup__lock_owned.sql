-- name: kubernetes_setup__lock_owned :one
SELECT setup.id,setup.version,setup.state
FROM control_plane.kubernetes_identity_setups setup
JOIN control_plane.integration_connections connection ON connection.id=setup.connection_id
 AND connection.organization_id=setup.organization_id AND connection.lifecycle_state='ACTIVE'
JOIN control_plane.subjects actor ON actor.id=setup.actor_id AND actor.organization_id=setup.organization_id
 AND actor.kind='USER' AND actor.active
WHERE setup.organization_id=@organization_id::uuid AND setup.actor_id=@actor_id::uuid
 AND connection.ref=@connection_ref AND setup.ref=@setup_ref
FOR UPDATE OF setup;
