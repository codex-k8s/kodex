-- name: ResultGrantKeysetAdmit
SELECT integration_gateway.admit_result_grant_keyset(
    @keyset_revision, @high_watermark, @served_generation,
    @keyset_sha256, @signer_generation
)
