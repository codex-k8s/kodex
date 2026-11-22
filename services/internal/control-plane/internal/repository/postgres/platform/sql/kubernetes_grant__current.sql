-- name: kubernetes_grant__current :one
SELECT grant_row.ref,grant_row.approval_policy,grant_row.approval_scope_paths,
 EXISTS(SELECT 1 FROM control_plane.integration_invocations invocation WHERE invocation.grant_id=grant_row.id
  AND invocation.organization_id=grant_row.organization_id AND invocation.state IN('WAITING_APPROVAL','READY','RUNNING','UNKNOWN_OUTCOME'))
FROM control_plane.integration_grants grant_row
WHERE grant_row.organization_id=@organization_id::uuid AND grant_row.connection_id=@connection_id::uuid
 AND grant_row.capability_key=@capability AND grant_row.target_kind=@target_kind AND grant_row.target_ref=@target_ref
 AND grant_row.resource_kind='KUBERNETES_TARGET' AND grant_row.resource_scope=@resource_scope::jsonb
 AND grant_row.resource_scope_digest=@resource_scope_digest AND (@grant_ref='' OR grant_row.ref=@grant_ref)
FOR UPDATE OF grant_row;
