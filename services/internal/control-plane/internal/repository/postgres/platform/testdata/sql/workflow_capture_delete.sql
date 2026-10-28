-- name: workflow_capture_delete :exec
DELETE FROM control_plane.run_nodes WHERE ref=@node_ref;
