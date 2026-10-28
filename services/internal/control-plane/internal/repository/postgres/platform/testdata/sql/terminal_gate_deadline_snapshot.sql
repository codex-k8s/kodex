-- name: terminal_gate_deadline_snapshot :one
WITH root AS (SELECT * FROM control_plane.runs WHERE ref=$1),
graph AS (SELECT run.id FROM control_plane.runs run JOIN root ON root.id=run.root_run_id)
SELECT jsonb_build_object(
 'runs',(SELECT jsonb_agg(to_jsonb(run) ORDER BY run.id) FROM control_plane.runs run WHERE run.id IN (SELECT id FROM graph)),
 'nodes',(SELECT jsonb_agg(to_jsonb(node) ORDER BY node.id) FROM control_plane.run_nodes node WHERE node.run_id IN (SELECT id FROM graph)),
 'edges',(SELECT jsonb_agg(to_jsonb(edge) ORDER BY edge.id) FROM control_plane.run_edges edge WHERE edge.root_run_id=root.id),
 'gates',(SELECT jsonb_agg(to_jsonb(gate) ORDER BY gate.id) FROM control_plane.owner_gates gate WHERE gate.root_run_id=root.id),
 'turns',(SELECT jsonb_agg(to_jsonb(turn) ORDER BY turn.id) FROM control_plane.session_turns turn WHERE turn.run_id IN (SELECT id FROM graph)),
 'leases',(SELECT jsonb_agg(to_jsonb(lease) ORDER BY lease.id) FROM control_plane.runtime_leases lease WHERE lease.run_id IN (SELECT id FROM graph)),
 'revisions',(SELECT jsonb_agg(to_jsonb(revision) ORDER BY revision.id) FROM control_plane.runtime_revisions revision WHERE revision.run_id IN (SELECT id FROM graph)),
 'invocations',(SELECT jsonb_agg(to_jsonb(invocation) ORDER BY invocation.id) FROM control_plane.integration_invocations invocation WHERE invocation.run_id IN (SELECT id FROM graph)),
 'events',(SELECT jsonb_agg(to_jsonb(event) ORDER BY event.sequence) FROM control_plane.run_events event WHERE event.root_run_id=root.id),
 'receipts',(SELECT count(*) FROM control_plane.idempotency_receipts receipt WHERE receipt.organization_id=root.organization_id),
 'audit',(SELECT count(*) FROM control_plane.audit_events audit WHERE audit.organization_id=root.organization_id),
 'outbox',(SELECT count(*) FROM control_plane.outbox_events outbox WHERE convert_from(outbox.payload,'UTF8')::jsonb->>'projectRef'=(SELECT ref FROM control_plane.projects WHERE id=root.project_id))
)
FROM root;
