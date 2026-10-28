-- name: workflow_capture_purge_proof :one
SELECT (SELECT count(*) FROM control_plane.run_nodes WHERE ref=@node_ref),
       (SELECT count(*) FROM control_plane.runs WHERE ref=@run_ref),
       (SELECT count(*) FROM control_plane.project_purge_targets),
       (SELECT count(*) FROM control_plane.project_purge_context),
       (SELECT state FROM control_plane.project_purge_receipts WHERE project_ref=@project_ref);
