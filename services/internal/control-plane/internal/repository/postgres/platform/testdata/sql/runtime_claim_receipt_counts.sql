SELECT
 (SELECT count(*) FROM control_plane.audit_events WHERE resource_ref=$1 AND resource_kind='RUN' AND action='controlplane.claim_execution'),
 (SELECT count(*) FROM control_plane.idempotency_receipts WHERE idempotency_key=$2),
 (SELECT count(*) FROM control_plane.run_events WHERE root_run_id=(SELECT id FROM control_plane.runs WHERE ref=$1));
