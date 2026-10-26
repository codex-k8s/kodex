-- name: legacy_workflow_rework__node :one
SELECT COALESCE(owner_rework_gate_id::text,'') FROM control_plane.run_nodes
WHERE organization_id=@organization_id::uuid AND id=@node_id::uuid;
