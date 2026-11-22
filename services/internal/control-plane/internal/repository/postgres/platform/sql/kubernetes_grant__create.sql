-- name: kubernetes_grant__create :one
INSERT INTO control_plane.integration_grants(
 ref,organization_id,connection_id,capability_key,target_kind,target_ref,enabled,approval_policy,created_by,
 risk,resource_kind,resource_scope,resource_scope_digest,definition_version,definition_digest,approval_scope_paths)
VALUES(@ref,@organization_id::uuid,@connection_id::uuid,@capability,@target_kind,@target_ref,true,@policy,@actor_id::uuid,
 @risk,'KUBERNETES_TARGET',@resource_scope::jsonb,@resource_scope_digest,@definition_version,@definition_digest,@scope_paths::text[])
RETURNING ref;
