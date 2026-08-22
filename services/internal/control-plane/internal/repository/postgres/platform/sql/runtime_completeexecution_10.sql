-- name: platform__runtime_completeexecution_10 :exec
INSERT INTO control_plane.callback_receipts(child_run_id,callback_edge_id) VALUES($1::uuid,$2::uuid) ON CONFLICT DO NOTHING
