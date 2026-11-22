-- name: ssh_runtime__lock :one
SELECT invocation.id::text FROM control_plane.integration_invocations invocation
JOIN control_plane.integration_connections connection ON connection.id=invocation.connection_id AND connection.organization_id=invocation.organization_id
JOIN control_plane.ssh_invocation_identity_bindings pin ON pin.invocation_id=invocation.id AND pin.organization_id=invocation.organization_id
WHERE invocation.ref=@invocation_ref AND invocation.organization_id=@organization_id::uuid
 AND invocation.resource_kind='SSH_TARGET' AND connection.definition_key IN('ssh-managed','ssh-execute')
 AND pin.connection_id=invocation.connection_id AND pin.root_actor_id=@actor_id::uuid
FOR UPDATE OF invocation;
