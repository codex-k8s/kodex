-- name: configuration_source__enqueue :exec
INSERT INTO control_plane.managed_configuration_source_work
 (ref,organization_id,source_id,source_generation,root_actor_id,connection_id,connection_version,
  credential_revision_id,input_snapshot,input_sha256,state,deadline)
VALUES (@ref,@organization_id::uuid,@source_id::uuid,@generation,@actor_id::uuid,@connection_id::uuid,@connection_version,
 @credential_id::uuid,@snapshot::jsonb,@digest,'QUEUED',@deadline);
