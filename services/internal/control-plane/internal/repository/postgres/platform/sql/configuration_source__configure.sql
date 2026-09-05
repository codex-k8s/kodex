-- name: configuration_source__configure :one
INSERT INTO control_plane.managed_configuration_git_sources
 (ref,organization_id,configuration_set_id,root_actor_id,connection_id,provider_key,repository_ref,ref_name,path,content_format,state)
VALUES (@ref,@organization_id::uuid,@configuration_id::uuid,@actor_id::uuid,@connection_id::uuid,@provider,@repository,@ref_name,@path,@format,'QUEUED')
ON CONFLICT(configuration_set_id) DO UPDATE SET root_actor_id=EXCLUDED.root_actor_id,
 connection_id=EXCLUDED.connection_id, provider_key=EXCLUDED.provider_key, repository_ref=EXCLUDED.repository_ref,
 ref_name=EXCLUDED.ref_name,path=EXCLUDED.path,content_format=EXCLUDED.content_format,
 version=managed_configuration_git_sources.version+1,generation=managed_configuration_git_sources.generation+1,
 state='QUEUED',accepted_commit_sha='',accepted_content_sha256='',accepted_revision_id=NULL,synced_at=NULL,
 failure_code='',next_refresh_at=clock_timestamp(),updated_at=clock_timestamp()
RETURNING id::text;
