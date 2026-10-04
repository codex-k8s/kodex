-- name: role_images_expiry_outcome_readback :one
SELECT
  (SELECT count(*) FROM control_plane.audit_events WHERE organization_id=$1::uuid AND resource_ref=$2 AND action='platform.role-images.builds.expire'),
  (SELECT count(*) FROM control_plane.outbox_events),
  (SELECT count(*) FROM control_plane.idempotency_receipts WHERE organization_id=$1::uuid AND actor_id=$3::uuid AND operation='platform.role-images.builds.claim' AND idempotency_key=$4),
  COALESCE((SELECT response_type FROM control_plane.idempotency_receipts WHERE organization_id=$1::uuid AND actor_id=$3::uuid AND operation='platform.role-images.builds.claim' AND idempotency_key=$4),'');
