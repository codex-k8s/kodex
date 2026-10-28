-- name: legacy_workflow_rework__callback_origin :one
SELECT callback_edge_id::text,callback_edge_ref,parent_node_id::text,parent_node_ref,parent_run_id::text
FROM control_plane.legacy_rework_callback_origin(@organization_id::uuid,@child_run_id::uuid,@runtime_revision_id::uuid)
WHERE eligible;
