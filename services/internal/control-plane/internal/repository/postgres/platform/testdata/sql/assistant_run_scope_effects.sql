-- name: assistant_run_scope_effects :one
SELECT
  (SELECT count(*) FROM control_plane.runs WHERE organization_id=$1::uuid),
  (SELECT count(*) FROM control_plane.idempotency_receipts WHERE organization_id=$1::uuid),
  (SELECT count(*) FROM control_plane.audit_events WHERE organization_id=$1::uuid),
  (SELECT count(*) FROM control_plane.outbox_events),
  (SELECT count(*) FROM control_plane.session_turns turn JOIN control_plane.sessions session ON session.id=turn.session_id WHERE session.organization_id=$1::uuid);
