-- name: assistant_workflow_frontier_pins :one
SELECT version.ref, version.spec, run.input
FROM control_plane.runs run
JOIN control_plane.workflow_versions version
  ON version.id = run.workflow_version_id
 AND version.organization_id = run.organization_id
WHERE run.organization_id = @organization_id::uuid
  AND run.ref = @run_ref
  AND run.root_run_id = run.id
  AND run.target_type = 'WORKFLOW';
