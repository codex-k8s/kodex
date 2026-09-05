-- name: configuration_writeback__insert :one
INSERT INTO control_plane.managed_configuration_writebacks
(ref,organization_id,configuration_set_id,source_id,root_actor_id,connection_id,credential_revision_id,input_snapshot,input_sha256,approval_digest,state)
VALUES (@ref,@organization_id::uuid,@configuration_id::uuid,@source_id::uuid,@actor_id::uuid,@connection_id::uuid,@credential_id::uuid,@snapshot::jsonb,@digest,@approval_digest,'WAITING_APPROVAL')
RETURNING id::text,created_at,expires_at;
