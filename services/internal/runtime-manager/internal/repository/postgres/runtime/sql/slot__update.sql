-- name: slot__update :exec
UPDATE runtime_manager_slots
SET
    status = @status,
    runtime_mode = @runtime_mode,
    is_prewarmed = @is_prewarmed,
    fleet_scope_id = @fleet_scope_id::uuid,
    cluster_id = @cluster_id::uuid,
    namespace_name = @namespace_name,
    agent_run_id = @agent_run_id::uuid,
    project_id = @project_id::uuid,
    repository_ids_json = @repository_ids_json::jsonb,
    runtime_profile = @runtime_profile,
    fingerprint = @fingerprint,
    lease_owner = @lease_owner,
    lease_until = @lease_until::timestamptz,
    last_error_code = @last_error_code,
    last_error_message = @last_error_message,
    version = @version,
    updated_at = @updated_at
WHERE id = @id
  AND version = @previous_version;
