-- name: workflow_effect_diagnostic :one
SELECT root.state,physical.state,node.state,
    (SELECT count(*) FROM control_plane.runtime_revisions revision WHERE revision.organization_id=root.organization_id AND revision.node_id=node.id),
    control_plane.repository_bundle_callback_pending(root.organization_id,physical.id),
    (SELECT count(*) FROM control_plane.repository_bundle_publications publication WHERE publication.organization_id=root.organization_id AND publication.origin_run_id=physical.id AND publication.state IN ('WAITING_APPROVAL','QUEUED','CLAIMED','EFFECT_STARTED','UNKNOWN_OUTCOME')),
    (SELECT count(*) FROM control_plane.repository_bundle_publications publication WHERE publication.organization_id=root.organization_id AND publication.origin_run_id=physical.id AND publication.continuation_state='BLOCKED'),
    (SELECT count(*) FROM control_plane.run_nodes model WHERE model.organization_id=root.organization_id AND model.run_id=physical.id AND model.type='AGENT_EXECUTION' AND model.state IN ('QUEUED','RUNNING','WAITING')),
    (SELECT count(*) FROM control_plane.run_edges callback WHERE callback.organization_id=root.organization_id AND callback.root_run_id=root.id AND callback.target_node_id=node.id AND callback.type='CALLBACK_TO' AND NOT EXISTS (SELECT 1 FROM control_plane.callback_receipts receipt WHERE receipt.callback_edge_id=callback.id)),
    (SELECT count(*) FROM control_plane.integration_invocations invocation WHERE invocation.organization_id=root.organization_id AND invocation.node_id=node.id AND invocation.state NOT IN ('SUCCEEDED','FAILED','REJECTED','CANCELLED')),
    (SELECT count(*) FROM control_plane.run_edges edge WHERE edge.organization_id=root.organization_id AND edge.root_run_id=root.id AND edge.source_node_id=node.id AND edge.type='CONTINUES')
FROM control_plane.runs root
JOIN control_plane.run_nodes node ON node.root_run_id=root.id AND node.organization_id=root.organization_id
JOIN control_plane.runs physical ON physical.id=node.run_id AND physical.organization_id=root.organization_id AND physical.root_run_id=root.id
WHERE root.organization_id=@organization_id::uuid AND root.id=@root_run_id::uuid AND node.id=@node_id::uuid;
