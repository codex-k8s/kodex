INSERT INTO control_plane.delegation_callback_manifests (
    id, plan_id, delegation_id, callback_process_id, destinations,
    manifest_sha256, created_at
) VALUES (
    @id::uuid, @plan_id::uuid, @delegation_id::uuid,
    @callback_process_id::uuid, @destinations, @manifest_sha256, @created_at
)
