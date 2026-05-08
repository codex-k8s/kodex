-- name: job__list :many
SELECT
    id,
    command_id,
    job_type,
    status,
    priority,
    job_input_json,
    lease_owner,
    lease_token_hash,
    lease_until,
    claim_attempt,
    slot_id,
    agent_run_id,
    project_id,
    repository_id,
    release_line_id,
    package_installation_id,
    fleet_scope_id,
    cluster_id,
    requested_by,
    created_at,
    started_at,
    finished_at,
    next_action,
    last_error_code,
    last_error_message,
    short_log_tail,
    full_log_ref,
    updated_at,
    version
FROM runtime_manager_jobs
WHERE (cardinality(@statuses::text[]) = 0 OR status = ANY(@statuses::text[]))
  AND (cardinality(@job_types::text[]) = 0 OR job_type = ANY(@job_types::text[]))
  AND (@project_id::uuid IS NULL OR project_id = @project_id::uuid)
  AND (@slot_id::uuid IS NULL OR slot_id = @slot_id::uuid)
  AND (@agent_run_id::uuid IS NULL OR agent_run_id = @agent_run_id::uuid)
  AND (@release_line_id::uuid IS NULL OR release_line_id = @release_line_id::uuid)
ORDER BY updated_at DESC, id
LIMIT @limit::integer OFFSET @offset::integer;
