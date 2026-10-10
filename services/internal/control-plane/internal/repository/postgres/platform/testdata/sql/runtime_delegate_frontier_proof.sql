-- name: runtime_delegate_frontier_proof :one
SELECT jsonb_build_object(
    'sessions', (SELECT count(*) FROM control_plane.sessions WHERE project_id = root.project_id),
    'runs', (SELECT count(*) FROM control_plane.runs WHERE project_id = root.project_id),
    'turns', (SELECT count(*) FROM control_plane.session_turns turn JOIN control_plane.sessions session ON session.id = turn.session_id WHERE session.project_id = root.project_id),
    'nodes', (SELECT count(*) FROM control_plane.run_nodes WHERE root_run_id = root.id),
    'edges', (SELECT count(*) FROM control_plane.run_edges edge JOIN control_plane.run_nodes source ON source.id = edge.source_node_id WHERE source.root_run_id = root.id),
    'revisions', (SELECT count(*) FROM control_plane.runtime_revisions revision JOIN control_plane.runs run ON run.id = revision.run_id WHERE run.project_id = root.project_id),
    'receipts', (SELECT count(*) FROM control_plane.idempotency_receipts WHERE organization_id = root.organization_id AND operation = 'controlplane.delegate_execution'),
    'audit', (SELECT count(*) FROM control_plane.audit_events WHERE organization_id = root.organization_id AND action = 'controlplane.delegate_execution'),
    'events', (SELECT count(*) FROM control_plane.run_events WHERE root_run_id = root.id)
)
FROM control_plane.runs root WHERE root.ref = @root_ref;
