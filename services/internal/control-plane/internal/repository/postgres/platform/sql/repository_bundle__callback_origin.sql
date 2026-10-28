-- name: repository_bundle__callback_origin :many
SELECT callback_edge_id::text,callback_edge_ref,parent_node_id::text,parent_node_ref,parent_run_id::text,eligible
FROM control_plane.repository_bundle_callback_origin(@organization_id::uuid,@child_run_id::uuid,@runtime_revision_id::uuid);
