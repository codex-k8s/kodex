-- name: runtime_delegate_input_missing :one
SELECT parent_run.initiated_by::text,
       parent_run.id::text,
       FALSE,
       parent_run.input,
       NULL::jsonb
FROM control_plane.runs parent_run
WHERE parent_run.id = @parent_run_id::uuid
  AND parent_run.root_run_id = @root_run_id::uuid
  AND parent_run.organization_id = @organization_id::uuid
  AND parent_run.project_id IS NOT DISTINCT FROM NULLIF(@project_id, '')::uuid
  AND FALSE
