-- name: kubernetes_grant__change :one
UPDATE control_plane.integration_grants
SET enabled=@enabled,approval_policy=@policy,approval_scope_paths=@scope_paths::text[],risk=@risk,
 definition_version=@definition_version,definition_digest=@definition_digest,version=version+1,updated_at=clock_timestamp()
WHERE organization_id=@organization_id::uuid AND connection_id=@connection_id::uuid AND ref=@ref
 AND capability_key=@capability AND target_kind=@target_kind AND target_ref=@target_ref
 AND resource_kind='KUBERNETES_TARGET' AND resource_scope=@resource_scope::jsonb AND resource_scope_digest=@resource_scope_digest
RETURNING ref;
