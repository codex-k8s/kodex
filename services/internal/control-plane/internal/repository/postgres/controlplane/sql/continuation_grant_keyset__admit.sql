-- name: ContinuationGrantKeysetAdmit
SELECT control_plane.admit_continuation_grant_keyset(
    @keyset_revision, @high_watermark, @served_generation,
    @keyset_sha256, @signer_generation
)
