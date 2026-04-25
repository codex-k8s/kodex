-- name: githubratelimitwait__claim_next_due_auto_resume_candidate :one
SELECT *
FROM github_rate_limit_waits
WHERE (
    (state IN ('open', 'auto_resume_scheduled') AND resume_not_before IS NOT NULL AND resume_not_before <= $1)
    OR (state = 'auto_resume_in_progress' AND last_resume_attempt_at IS NOT NULL AND last_resume_attempt_at <= $2)
)
ORDER BY
    CASE WHEN state = 'auto_resume_in_progress' THEN 0 ELSE 1 END,
    resume_not_before ASC NULLS FIRST,
    updated_at ASC,
    id ASC
FOR UPDATE SKIP LOCKED
LIMIT 1;
