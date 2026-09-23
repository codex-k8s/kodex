-- name: project_trash__active_run_roots :many
SELECT DISTINCT ON (run.root_run_id) run.ref, run.version
FROM control_plane.runs run
WHERE run.organization_id=@organization_id::uuid
  AND run.project_id=@project_id::uuid
  AND run.state IN ('QUEUED', 'RUNNING', 'WAITING_HUMAN', 'CANCELLING')
ORDER BY run.root_run_id, (run.id=run.root_run_id) DESC, run.id
