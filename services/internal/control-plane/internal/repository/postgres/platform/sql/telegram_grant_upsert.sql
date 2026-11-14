-- name: telegram_grant_upsert :one
INSERT INTO control_plane.integration_grants(ref,organization_id,connection_id,telegram_project_id,capability_key,
 target_kind,target_ref,enabled,approval_policy,created_by,risk,resource_kind,resource_scope,resource_scope_digest,
 definition_version,definition_digest,approval_scope_paths)
VALUES(@ref,@organization_id::uuid,@connection_id::uuid,@project_id::uuid,'telegram.notifications',
 'TELEGRAM_SUBSCRIPTION',@subscription_ref,true,'HUMAN_SCOPED',@actor_id::uuid,'WRITE','HTTPS_RESOURCE',@scope::jsonb,@scope_digest,
 @definition_version,@definition_digest,'{}'::text[])
ON CONFLICT(connection_id,capability_key,target_kind,target_ref) DO UPDATE SET enabled=true,
 resource_scope=EXCLUDED.resource_scope,resource_scope_digest=EXCLUDED.resource_scope_digest,
 definition_version=EXCLUDED.definition_version,definition_digest=EXCLUDED.definition_digest,
 version=control_plane.integration_grants.version+1,updated_at=clock_timestamp()
RETURNING ref;
