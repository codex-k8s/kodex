-- name: platform__runtime_completeexecution_select_run_edges_root_run_id_source_node_id_type :one
SELECT id::text,ref,target_node_id::text FROM control_plane.run_edges WHERE root_run_id=$1::uuid AND source_node_id=$2::uuid AND type='CALLBACK_TO' ORDER BY created_at LIMIT 1
