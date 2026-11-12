-- name: workspace_claim_contention_state :one
SELECT jsonb_build_object(
 'state',run.state,'version',run.version,'sequence',run.event_sequence,
 'nodes',(SELECT jsonb_agg(jsonb_build_object('ref',node.ref,'state',node.state,'version',node.version) ORDER BY node.ref)
   FROM control_plane.run_nodes node WHERE node.run_id=run.id),
 'revisions',(SELECT count(*) FROM control_plane.runtime_revisions revision WHERE revision.run_id=run.id),
 'leases',(SELECT count(*) FROM control_plane.runtime_leases lease WHERE lease.run_id=run.id),
 'workspace',(SELECT jsonb_build_object('state',workspace.state,'generation',workspace.generation,'version',workspace.version,
   'lease',workspace.writer_lease_id,'released',workspace.writer_released)
   FROM control_plane.session_workspace_storage workspace WHERE workspace.session_id=run.session_id),
 'audits',(SELECT count(*) FROM control_plane.audit_events audit WHERE audit.resource_ref=run.ref),
 'receipts',(SELECT count(*) FROM control_plane.idempotency_receipts receipt WHERE receipt.idempotency_key=$2))
FROM control_plane.runs run WHERE run.ref=$1;
