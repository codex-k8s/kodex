-- name: runtime_delegateexecution_insert_runs_ref_project_id_root_run_id :one
INSERT INTO control_plane.runs(
    ref, organization_id, project_id, session_id, root_run_id, parent_run_id,
    workflow_version_id, target_type, target_ref, source, title, task, input,
    state, initiated_by, started_at
)
SELECT @run_ref,
       @organization_id::uuid,
       @project_id::uuid,
       @session_id::uuid,
       root.id,
       @parent_run_id::uuid,
       root.workflow_version_id,
       'AGENT',
       @target_agent_ref,
       'AGENT_DELEGATION',
       @title,
       @task,
       @input,
       'RUNNING',
       @initiated_by::uuid,
       clock_timestamp()
FROM control_plane.runs root
WHERE root.id = @root_run_id::uuid
  AND root.organization_id = @organization_id::uuid
RETURNING id::text
