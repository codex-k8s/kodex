-- name: integration_invocation__broker_kind :one
SELECT connection.definition_key IN('kubernetes-managed','ssh-managed','ssh-execute')
FROM control_plane.integration_invocations invocation
JOIN control_plane.integration_connections connection ON connection.id=invocation.connection_id AND connection.organization_id=invocation.organization_id
WHERE invocation.organization_id=@organization_id::uuid AND invocation.ref=@invocation_ref;
