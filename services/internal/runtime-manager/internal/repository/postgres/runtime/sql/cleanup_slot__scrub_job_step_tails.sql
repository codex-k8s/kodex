-- name: cleanup_slot__scrub_job_step_tails :exec
UPDATE runtime_manager_job_steps step
SET
    short_log_tail = '',
    updated_at = @now::timestamptz,
    version = step.version + 1
FROM runtime_manager_jobs job
WHERE step.job_id = job.id
  AND job.slot_id = @slot_id
  AND step.short_log_tail <> '';
