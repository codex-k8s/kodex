-- name: workflow_launch__bind_parent :exec
UPDATE control_plane.runs SET parent_run_id=@parent_run_id::uuid WHERE organization_id=@organization_id::uuid AND ref=@child_ref AND parent_run_id IS NULL AND root_run_id=id;
