-- name: job_step__list_by_job_ids :many
SELECT
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
FROM runtime_manager_job_steps
WHERE job_id = ANY(@job_ids::uuid[])
ORDER BY job_id, created_at, id;
