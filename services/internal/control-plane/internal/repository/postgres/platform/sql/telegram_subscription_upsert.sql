-- name: telegram_subscription_upsert :one
INSERT INTO control_plane.telegram_subscriptions(ref,organization_id,project_id,connection_id,connection_version,
 credential_revision_id,definition_version,definition_digest,configuration,event_types,scope_digest,enabled,
 approved_by,expires_at,max_effects)
VALUES(@ref,@organization_id::uuid,@project_id::uuid,@connection_id::uuid,@connection_version,
 @credential_revision_id::uuid,@definition_version,@definition_digest,@configuration::jsonb,@events::text[],@scope_digest,true,
 @actor_id::uuid,clock_timestamp()+make_interval(secs=>@duration_seconds),@max_effects)
ON CONFLICT(project_id) DO UPDATE SET connection_id=EXCLUDED.connection_id,connection_version=EXCLUDED.connection_version,
 credential_revision_id=EXCLUDED.credential_revision_id,definition_version=EXCLUDED.definition_version,
 definition_digest=EXCLUDED.definition_digest,configuration=EXCLUDED.configuration,event_types=EXCLUDED.event_types,
 scope_digest=EXCLUDED.scope_digest,enabled=true,approved_by=EXCLUDED.approved_by,expires_at=EXCLUDED.expires_at,
 max_effects=EXCLUDED.max_effects,used_effects=0,version=control_plane.telegram_subscriptions.version+1,
 updated_at=clock_timestamp()
RETURNING id::text,ref;
