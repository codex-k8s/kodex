-- name: cleanup_slot__scrub_job_tails :exec
UPDATE runtime_manager_jobs
SET
    short_log_tail = '',
    updated_at = @now::timestamptz,
    version = version + 1
WHERE slot_id = @slot_id
  AND short_log_tail <> '';
