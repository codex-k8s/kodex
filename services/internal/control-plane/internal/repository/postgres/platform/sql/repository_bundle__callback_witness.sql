-- name: repository_bundle__callback_witness :one
SELECT publication.ref,node.ref,publication.origin_node_id=node.id
FROM control_plane.run_nodes node
JOIN control_plane.repository_bundle_publications publication
  ON publication.organization_id=node.organization_id AND publication.root_run_id=node.root_run_id
WHERE node.organization_id=@organization_id::uuid AND node.id=@node_id::uuid
  AND (publication.origin_node_id=node.id OR EXISTS (
    SELECT 1 FROM control_plane.run_edges callback
    JOIN control_plane.callback_receipts receipt ON receipt.callback_edge_id=callback.id
      AND receipt.child_run_id=publication.origin_run_id
    JOIN control_plane.runs receipt_child ON receipt_child.id=receipt.child_run_id
      AND receipt_child.organization_id=node.organization_id AND receipt_child.root_run_id=node.root_run_id
      AND receipt_child.project_id=publication.project_id
    WHERE callback.organization_id=node.organization_id AND callback.root_run_id=node.root_run_id
      AND callback.target_node_id=node.id AND callback.type='CALLBACK_TO'))
ORDER BY (publication.origin_node_id=node.id) DESC,publication.created_at DESC,publication.id DESC
LIMIT 1;
