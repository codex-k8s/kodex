UPDATE control_plane.runtime_executions
SET version = @version,
    fence = @fence,
    state = @state,
    lease_id = nullif(@lease_id, '')::uuid,
    lease_token_sha256 = nullif(@lease_token_sha256, ''),
    lease_expires_at = nullif(@lease_expires_at, 'epoch'::timestamptz),
    terminal_outcome = nullif(@terminal_outcome, ''),
    terminal_reference = nullif(@terminal_reference, ''),
    terminal_sha256 = nullif(@terminal_sha256, ''),
    archive_reference = nullif(@archive_reference, ''),
    archive_sha256 = nullif(@archive_sha256, ''),
    restore_proof_reference = nullif(@restore_proof_reference, ''),
    restore_proof_sha256 = nullif(@restore_proof_sha256, ''),
    restore_verifier_workload_id = nullif(@restore_verifier_workload_id, ''),
    cleanup_authorization_id = nullif(@cleanup_authorization_id, '')::uuid,
    cleanup_authorization_expires_at =
        nullif(@cleanup_authorization_expires_at, 'epoch'::timestamptz),
    updated_at = @updated_at
WHERE id = @id
  AND version = @expected_version
  AND fence = @expected_fence;
