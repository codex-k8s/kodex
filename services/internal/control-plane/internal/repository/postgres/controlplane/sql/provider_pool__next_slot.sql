-- name: ProviderPoolNextSlot
SELECT control_plane.next_provider_pool_slot(
    @role_id::uuid,
    @policy_revision,
    @snapshot_sha256,
    @total_weight
)
