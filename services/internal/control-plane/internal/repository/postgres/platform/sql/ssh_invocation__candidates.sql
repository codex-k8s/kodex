-- name: ssh_invocation__candidates :many
SELECT invocation.ref,invocation.created_at
FROM control_plane.integration_invocations invocation
JOIN control_plane.integration_connections connection ON connection.id=invocation.connection_id AND connection.organization_id=invocation.organization_id
WHERE invocation.organization_id=@organization_id::uuid AND connection.definition_key IN('ssh-managed','ssh-execute')
 AND invocation.state='READY' AND invocation.generation=0
 AND (invocation.created_at,invocation.ref)>(@after_created_at::timestamptz,@after_ref::text)
ORDER BY invocation.created_at,invocation.ref LIMIT 32;
