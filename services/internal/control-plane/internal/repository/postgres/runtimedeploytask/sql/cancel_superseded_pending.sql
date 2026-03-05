-- name: runtimedeploytask__cancel_superseded_pending :exec
WITH current_task AS (
    SELECT
        run_id,
        repository_full_name,
        target_env,
        runtime_mode,
        namespace,
        slot_no,
        created_at
    FROM runtime_deploy_tasks
    WHERE run_id = $1::uuid
)
UPDATE runtime_deploy_tasks t
SET
    status = 'canceled',
    lease_owner = NULL,
    lease_until = NULL,
    last_error = $2::text,
    finished_at = NOW(),
    updated_at = NOW()
FROM current_task c
WHERE t.run_id <> c.run_id
  AND t.status = 'pending'
  AND t.repository_full_name = c.repository_full_name
  AND t.target_env = c.target_env
  AND t.runtime_mode = c.runtime_mode
  AND t.created_at <= c.created_at
  AND (
      (
          NULLIF(c.namespace, '') IS NOT NULL
          AND NULLIF(t.namespace, '') IS NOT NULL
          AND t.namespace = c.namespace
      )
      OR (
          (
              NULLIF(c.namespace, '') IS NULL
              OR NULLIF(t.namespace, '') IS NULL
          )
          AND t.slot_no = c.slot_no
      )
  );
