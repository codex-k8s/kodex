-- name: secret_draft_operation_insert :exec
INSERT INTO control_plane.runtime_secret_draft_operations
(ref,organization_id,draft_id,actor_id,kind,expected_draft_version,expected_secret_version,expected_current_revision,
target_revision,token_digest,idempotency_key,intent_digest,grant_expires_at,correlation_ref)
VALUES(@ref,@organization_id::uuid,@draft_id::uuid,@actor_id::uuid,@kind,@draft_version,@secret_version,
@current_revision,@target_revision,@token_digest,@idempotency_key,@intent_digest,
@grant_expires_at,@correlation_ref);
