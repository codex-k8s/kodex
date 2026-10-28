-- name: workflow_exception__source :one
SELECT node.run_id::text,node.workflow_inputs FROM control_plane.run_nodes node
WHERE node.organization_id=@organization_id::uuid AND node.root_run_id=@root_run_id::uuid AND node.id=@node_id::uuid;
