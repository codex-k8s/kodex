INSERT INTO control_plane.delegation_callback_deliveries (
    id, plan_id, manifest_id, destination, receipt_sha256, state, delivered_at
) VALUES (
    @id::uuid, @plan_id::uuid, @manifest_id::uuid, @destination,
    @receipt_sha256, @state, @delivered_at
)
