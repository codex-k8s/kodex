-- name: runtime_secret_operation_insert :exec
INSERT INTO control_plane.runtime_secret_operations
  (ref, organization_id, project_id, actor_id, secret_id, kind, target_revision,
   expected_secret_version, expected_current_revision, expected_content_sha256,
   token_digest, idempotency_key, intent_digest, correlation_ref, state, grant_expires_at)
VALUES
  (@ref, @organization_id::uuid, @project_id::uuid, @actor_id::uuid, @secret_id::uuid, @kind,
   @target_revision, @expected_secret_version, @expected_current_revision, @expected_content_sha256,
   @token_digest, @idempotency_key, @intent_digest, @correlation_ref, 'PREPARED', @grant_expires_at);
