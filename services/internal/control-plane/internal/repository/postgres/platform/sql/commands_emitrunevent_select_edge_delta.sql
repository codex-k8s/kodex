-- name: platform__commands_emitrunevent_select_edge_delta :one
SELECT edge.ref,
       root.ref,
       source.ref,
       target.ref,
       edge.type,
       edge.label
FROM control_plane.run_edges edge
JOIN control_plane.runs root ON root.id = edge.root_run_id
JOIN control_plane.run_nodes source ON source.id = edge.source_node_id
JOIN control_plane.run_nodes target ON target.id = edge.target_node_id
WHERE edge.organization_id = $1::uuid
  AND edge.ref = $2
