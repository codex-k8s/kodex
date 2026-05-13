-- name: provider_operation__complete :one
UPDATE provider_hub_operations
SET
    status = @status,
    result_ref = @result_ref,
    error_code = @error_code,
    error_message = @error_message,
    rate_limit_snapshot_id = @rate_limit_snapshot_id,
    provider_version = @provider_version,
    finished_at = @finished_at,
    version = version + 1,
    updated_at = @updated_at
WHERE id = @id
    AND status = 'in_progress'
RETURNING
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
    operation_policy_context_json,
    approval_gate_ref_json,
    provider_version,
    started_at,
    finished_at,
    version,
    created_at,
    updated_at;
