-- name: runtime_completeexecution_select_run_edges_root_run_id_source_node_id_type :one
SELECT edge.id::text, edge.ref, edge.target_node_id::text, target.ref
FROM control_plane.run_edges edge
JOIN control_plane.run_nodes target ON target.id = edge.target_node_id
WHERE edge.root_run_id = $1::uuid
  AND edge.source_node_id = $2::uuid
  AND edge.type = 'CALLBACK_TO'
ORDER BY edge.created_at
LIMIT 1
