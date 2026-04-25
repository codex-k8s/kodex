-- name: runqueue__list_non_terminal_by_run_ids :many
SELECT id::text,
       status
FROM agent_runs
WHERE id::text = ANY($1::text[])
  AND status NOT IN ('succeeded', 'failed', 'canceled')
ORDER BY created_at ASC;
