-- name: runtime_callback_insert_continues_edge :exec
INSERT INTO control_plane.run_edges(
    ref, organization_id, root_run_id, source_node_id, target_node_id, type, label
) VALUES (
    @edge_ref, @organization_id::uuid, @root_run_id::uuid,
    @source_node_id::uuid, @target_node_id::uuid, 'CONTINUES', ''
)
