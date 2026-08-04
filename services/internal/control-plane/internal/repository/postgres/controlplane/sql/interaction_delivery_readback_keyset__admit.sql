SELECT keyset_revision, high_watermark, served_generation, keyset_sha256
FROM control_plane.admit_interaction_delivery_readback_keyset(
    @keyset_revision, @high_watermark, @served_generation, @keyset_sha256, @key_identities::jsonb
)
