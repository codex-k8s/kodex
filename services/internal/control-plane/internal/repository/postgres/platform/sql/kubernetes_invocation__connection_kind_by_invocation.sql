-- name: kubernetes_invocation__connection_kind_by_invocation :one
SELECT EXISTS(SELECT 1 FROM control_plane.integration_invocations invocation
 JOIN control_plane.integration_connections connection ON connection.id=invocation.connection_id
  AND connection.organization_id=invocation.organization_id
 WHERE invocation.organization_id=@organization_id::uuid AND invocation.ref=@invocation_ref
  AND connection.definition_key='kubernetes-managed');
