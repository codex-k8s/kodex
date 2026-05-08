-- name: job__update :exec
UPDATE runtime_manager_jobs
SET
    command_id = @command_id,
    job_type = @job_type,
    status = @status,
    priority = @priority,
    job_input_json = @job_input_json::jsonb,
    lease_owner = @lease_owner,
    lease_token_hash = @lease_token_hash,
    lease_until = @lease_until,
    claim_attempt = @claim_attempt,
    slot_id = @slot_id::uuid,
    agent_run_id = @agent_run_id::uuid,
    project_id = @project_id::uuid,
    repository_id = @repository_id::uuid,
    release_line_id = @release_line_id::uuid,
    package_installation_id = @package_installation_id::uuid,
    fleet_scope_id = @fleet_scope_id::uuid,
    cluster_id = @cluster_id::uuid,
    requested_by = @requested_by::uuid,
    started_at = @started_at,
    finished_at = @finished_at,
    next_action = @next_action,
    last_error_code = @last_error_code,
    last_error_message = @last_error_message,
    short_log_tail = @short_log_tail,
    full_log_ref = @full_log_ref,
    updated_at = @updated_at,
    version = @version
WHERE id = @id
  AND version = @previous_version;
