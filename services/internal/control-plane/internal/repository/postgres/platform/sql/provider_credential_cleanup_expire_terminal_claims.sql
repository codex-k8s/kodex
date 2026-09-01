-- name: provider_credential_cleanup_expire_terminal_claims :exec
UPDATE control_plane.provider_credential_cleanup_tasks task
SET state = 'DEAD_LETTER',
    lease_owner = NULL,
    lease_expires_at = NULL,
    safe_error_code = 'PROVIDER_CREDENTIAL_CLEANUP_LEASE_EXPIRED',
    terminal_receipt = 'dead-letter:' || task.ref || ':g' || task.lease_generation::text
        || ':a' || task.attempts::text || ':PROVIDER_CREDENTIAL_CLEANUP_LEASE_EXPIRED',
    completed_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE task.state = 'CLAIMED'
  AND task.lease_expires_at <= clock_timestamp()
  AND task.attempts >= task.maximum_attempts;
