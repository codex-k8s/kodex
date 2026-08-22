-- name: platform__runtime_delivercallback_insert_callback_receipts_child_run_id :exec
INSERT INTO control_plane.callback_receipts(child_run_id,callback_edge_id) VALUES($1::uuid,$2::uuid) ON CONFLICT DO NOTHING
