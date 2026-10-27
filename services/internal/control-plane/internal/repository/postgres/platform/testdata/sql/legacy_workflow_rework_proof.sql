-- name: legacy_workflow_rework_proof :one
SELECT root.state,child.state,source.state,
    (SELECT count(*) FROM control_plane.run_nodes model WHERE model.root_run_id=root.id AND model.type='AGENT_EXECUTION' AND model.state IN ('QUEUED','RUNNING')),
    (SELECT count(*) FROM control_plane.callback_receipts receipt JOIN control_plane.runs delivered ON delivered.id=receipt.child_run_id WHERE delivered.root_run_id=root.id),
    COALESCE((SELECT receipt.result_snapshot::text FROM control_plane.callback_receipts receipt WHERE receipt.child_run_id=child.id),'NONE'),
    (SELECT count(*) FROM control_plane.runtime_revisions revision WHERE revision.node_id=source.id),
    (SELECT count(*) FROM control_plane.run_edges edge WHERE edge.root_run_id=root.id AND edge.type='CONTINUES'),
    (SELECT count(*) FROM control_plane.run_events event WHERE event.root_run_id=root.id),
    (SELECT count(*) FROM control_plane.runs attempt WHERE attempt.root_run_id=root.id),
    (SELECT count(*) FROM control_plane.run_events event WHERE event.root_run_id=root.id AND event.run_state='CANCELLED')
FROM control_plane.runs root
JOIN control_plane.runs child ON child.root_run_id=root.id AND child.organization_id=root.organization_id
JOIN control_plane.run_nodes source ON source.run_id=child.id AND source.organization_id=root.organization_id
WHERE root.ref=@root_ref AND child.ref=@child_ref AND source.ref=@node_ref;
