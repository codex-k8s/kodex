-- name: runtime_callback_receipt_exists :one
SELECT EXISTS (
    SELECT 1 FROM control_plane.callback_receipts receipt
    JOIN control_plane.runs child ON child.id=receipt.child_run_id AND child.organization_id=@organization_id::uuid
    JOIN control_plane.run_edges edge ON edge.id=receipt.callback_edge_id AND edge.organization_id=child.organization_id
    WHERE receipt.child_run_id=@child_run_id::uuid AND receipt.callback_edge_id=@callback_edge_id::uuid
);
