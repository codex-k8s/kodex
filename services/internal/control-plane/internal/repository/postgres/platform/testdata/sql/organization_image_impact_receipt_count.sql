SELECT count(*)
FROM control_plane.idempotency_receipts
WHERE organization_id = $1::uuid AND actor_id = $2::uuid;
