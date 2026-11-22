-- name: kubernetes_setup__insert :exec
INSERT INTO control_plane.kubernetes_identity_setups(
 ref,organization_id,connection_id,actor_id,identity_revision_id,registry_revision,target_ref,target_revision,
 expected_connection_version,expected_binding_version,expected_binding_generation,definition_digest,configuration_digest,
 begin_idempotency_key,intent_sha256,permit_sha256,snapshot_sha256,content_sha256,content_bytes,created_at,expires_at)
VALUES(@ref,@organization_id::uuid,@connection_id::uuid,@actor_id::uuid,@identity_id::uuid,@registry_revision,@target_ref,@target_revision,
 @connection_version,@binding_version,@binding_generation,@definition_digest,@configuration_digest,
 @begin_key,@intent_digest,@permit_digest,@snapshot_digest,@content_digest,@content_bytes,@created_at,@expires_at);
