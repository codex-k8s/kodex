-- name: system_assistant_grant_side_effects :one
SELECT
  (SELECT count(*) FROM control_plane.integration_grants WHERE organization_id=$1::uuid),
  (SELECT count(*) FROM control_plane.idempotency_receipts WHERE organization_id=$1::uuid),
  (SELECT count(*) FROM control_plane.audit_events WHERE organization_id=$1::uuid),
  (SELECT count(*) FROM control_plane.outbox_events),
  (SELECT count(*) FROM control_plane.owner_gates WHERE organization_id=$1::uuid AND state='OPEN');
