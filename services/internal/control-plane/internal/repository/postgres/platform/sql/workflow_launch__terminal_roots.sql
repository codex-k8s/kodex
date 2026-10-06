-- name: workflow_launch__terminal_roots :many
SELECT run.id::text,run.ref,run.project_id::text,run.state
FROM control_plane.runs run
WHERE run.organization_id=@organization_id::uuid AND run.project_id=ANY(@project_ids::uuid[])
 AND run.id=run.root_run_id AND run.state IN ('SUCCEEDED','FAILED','CANCELLED')
 AND EXISTS (SELECT 1 FROM control_plane.required_workflow_launches launch
 WHERE launch.organization_id=run.organization_id AND (launch.origin_root_run_id=run.id OR launch.child_root_run_id=run.id))
ORDER BY run.id;
