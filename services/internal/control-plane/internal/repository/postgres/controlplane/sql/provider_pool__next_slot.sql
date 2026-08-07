-- name: ProviderPoolNextSlot
SELECT control_plane.next_provider_pool_slot(
    @selection_key_id::uuid,
    @policy_revision,
    @snapshot_sha256,
    @total_weight
)
