-- name: workflow_launch__failed :one
SELECT EXISTS(SELECT 1 FROM control_plane.required_workflow_launches WHERE origin_root_run_id=@root_run_id::uuid AND state IN ('FAILED','CANCELLED'));
