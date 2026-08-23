-- name: runtime_delegateexecution_select_runs_id :one
SELECT parent_session.provider_account_id::text,
       parent_run.initiated_by::text,
       parent_run.id::text
FROM control_plane.runs parent_run
JOIN control_plane.sessions parent_session ON parent_session.id = parent_run.session_id
WHERE parent_run.id = @parent_run_id::uuid
  AND parent_run.organization_id = @organization_id::uuid
