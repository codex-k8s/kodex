-- name: runtime_secret_operation_lock_by_digest :one
SELECT operation.id::text, operation.ref, operation.kind, operation.state,
       operation.project_id::text, operation.secret_id::text, operation.actor_id::text,
       operation.correlation_ref, operation.target_revision, operation.expected_secret_version,
       operation.expected_current_revision, COALESCE(operation.expected_content_sha256, ''),
       operation.grant_expires_at, COALESCE(operation.claimant_id, ''), operation.claim_generation,
       operation.claim_lease_deadline, COALESCE(operation.terminal_error_code, ''),
       operation.terminal_secret_snapshot, operation.intent_digest,
       project.ref, secret.ref, secret.version, secret.state, secret.current_revision,
       secret.name, secret.description, secret.value_type, secret.namespace,
       secret.created_at, secret.updated_at
FROM control_plane.runtime_secret_operations operation
JOIN control_plane.projects project ON project.id = operation.project_id
JOIN control_plane.runtime_secrets secret ON secret.id = operation.secret_id
WHERE operation.token_digest = @token_digest
  AND operation.organization_id = @organization_id::uuid
FOR UPDATE OF operation, secret;
