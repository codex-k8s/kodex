-- name: workspace_materialization__list :many
SELECT
    wm.id,
    wm.slot_id,
    wm.status,
    wm.policy_digest,
    wm.sources_json,
    wm.fingerprint,
    wm.started_at,
    wm.finished_at,
    wm.last_error_code,
    wm.last_error_message,
    wm.version,
    wm.created_at,
    wm.updated_at
FROM runtime_manager_workspace_materializations wm
JOIN runtime_manager_slots s ON s.id = wm.slot_id
WHERE (@slot_id::uuid IS NULL OR wm.slot_id = @slot_id::uuid)
  AND (@agent_run_id::uuid IS NULL OR s.agent_run_id = @agent_run_id::uuid)
  AND (cardinality(@statuses::text[]) = 0 OR wm.status = ANY(@statuses::text[]))
ORDER BY wm.updated_at DESC, wm.id
LIMIT @limit::integer OFFSET @offset::integer;
