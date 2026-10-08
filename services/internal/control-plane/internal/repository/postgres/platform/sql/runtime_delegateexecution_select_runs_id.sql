-- name: runtime_delegateexecution_select_runs_id :one
SELECT parent_run.initiated_by::text,
       parent_run.id::text,
       root.workflow_version_id IS NOT NULL,
       root.input,
       version.spec
FROM control_plane.runs parent_run
JOIN control_plane.runs root
  ON root.id = parent_run.root_run_id
 AND root.id = @root_run_id::uuid
 AND root.root_run_id = root.id
 AND root.organization_id = parent_run.organization_id
 AND root.project_id IS NOT DISTINCT FROM parent_run.project_id
 AND root.workflow_version_id IS NOT DISTINCT FROM parent_run.workflow_version_id
LEFT JOIN control_plane.workflow_versions version
  ON version.id = root.workflow_version_id
 AND version.organization_id = root.organization_id
LEFT JOIN control_plane.workflows workflow
  ON workflow.id = version.workflow_id
 AND workflow.organization_id = root.organization_id
 AND workflow.project_id = root.project_id
 AND workflow.ref = root.target_ref
WHERE parent_run.id = @parent_run_id::uuid
  AND parent_run.organization_id = @organization_id::uuid
  AND parent_run.project_id IS NOT DISTINCT FROM NULLIF(@project_id, '')::uuid
  AND (root.workflow_version_id IS NULL
       OR (root.target_type = 'WORKFLOW' AND workflow.id IS NOT NULL))
FOR UPDATE OF parent_run, root
