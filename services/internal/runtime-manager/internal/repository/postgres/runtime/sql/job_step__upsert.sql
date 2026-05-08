-- name: job_step__upsert :exec
INSERT INTO runtime_manager_job_steps (
    id,
    job_id,
    step_key,
    status,
    started_at,
    finished_at,
    short_log_tail,
    external_ref,
    error_code,
    error_message,
    version,
    created_at,
    updated_at
) VALUES (
    @id,
    @job_id,
    @step_key,
    @status,
    @started_at,
    @finished_at,
    @short_log_tail,
    @external_ref,
    @error_code,
    @error_message,
    @version,
    @created_at,
    @updated_at
)
ON CONFLICT (job_id, step_key) DO UPDATE
SET
    status = EXCLUDED.status,
    started_at = EXCLUDED.started_at,
    finished_at = EXCLUDED.finished_at,
    short_log_tail = EXCLUDED.short_log_tail,
    external_ref = EXCLUDED.external_ref,
    error_code = EXCLUDED.error_code,
    error_message = EXCLUDED.error_message,
    version = EXCLUDED.version,
    updated_at = EXCLUDED.updated_at;
