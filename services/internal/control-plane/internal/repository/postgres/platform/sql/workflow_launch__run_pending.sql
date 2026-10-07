-- name: workflow_launch__run_pending :one
SELECT EXISTS(SELECT 1 FROM control_plane.required_workflow_launches WHERE origin_run_id=@run_id::uuid AND state='OPEN'),
 EXISTS(SELECT 1 FROM control_plane.required_workflow_launches WHERE origin_node_id=@node_id::uuid);
