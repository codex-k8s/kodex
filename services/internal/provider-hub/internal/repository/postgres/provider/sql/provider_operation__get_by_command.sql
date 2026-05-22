-- name: provider_operation__get_by_command :one
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
    provider_object_id,
    repository_full_name,
    error_code,
    error_message,
    rate_limit_snapshot_id,
    operation_policy_context_json,
    approval_gate_ref_json,
    provider_version,
    base_branch,
    started_at,
    finished_at,
    version,
    created_at,
    updated_at
FROM provider_hub_operations
WHERE operation_type = @operation_type
  AND command_id = @command_id
  AND @command_id <> ''
LIMIT 1;
