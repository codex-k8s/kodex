-- name: provider_operation__get_replay :one
SELECT
    id,
    command_id,
    actor_id,
    external_account_id,
    provider_slug,
    operation_type,
    target_ref,
    status,
    result_ref,
    error_code,
    error_message,
    rate_limit_snapshot_id,
    started_at,
    finished_at,
    version,
    created_at,
    updated_at
FROM provider_hub_operations
WHERE @command_id <> ''
    AND operation_type = @operation_type
    AND command_id = @command_id
    AND actor_id IS NOT DISTINCT FROM @actor_id
    AND external_account_id = @external_account_id
    AND provider_slug = @provider_slug
    AND target_ref = @target_ref
    AND status = @status
    AND result_ref = @result_ref
    AND error_code = @error_code
    AND error_message = @error_message
    AND rate_limit_snapshot_id IS NOT DISTINCT FROM @rate_limit_snapshot_id;
