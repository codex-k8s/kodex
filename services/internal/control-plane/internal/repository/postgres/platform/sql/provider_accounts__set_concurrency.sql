-- name: provider_accounts__set_concurrency :exec
UPDATE control_plane.provider_accounts
SET max_concurrent_executions = @maximum_concurrent_executions,
    version = version + 1,
    updated_at = clock_timestamp()
WHERE id = @account_id::uuid
  AND organization_id = @organization_id::uuid
  AND version = @expected_version
  AND state NOT IN ('DELETING', 'DELETED');
