-- name: repository_bundle_child_fixture_state :one
SELECT child.state,root.state,
       (SELECT count(*)::integer FROM control_plane.callback_receipts receipt WHERE receipt.child_run_id=child.id),
       (SELECT count(*)::integer FROM control_plane.run_edges edge JOIN control_plane.run_nodes node ON node.id=edge.source_node_id
         WHERE edge.root_run_id=root.id AND edge.type='CONTINUES' AND node.ref=$2),
       COALESCE((SELECT receipt.result_snapshot->'artifacts' FROM control_plane.callback_receipts receipt WHERE receipt.child_run_id=child.id),'[]'::jsonb)
FROM control_plane.runs child JOIN control_plane.runs root ON root.id=child.root_run_id
WHERE child.ref=$1;
