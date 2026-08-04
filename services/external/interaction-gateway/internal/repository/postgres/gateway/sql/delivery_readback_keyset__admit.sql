SELECT keyset_revision, high_watermark, served_generation, keyset_sha256, retired_generations
FROM interaction_gateway_admit_delivery_readback_keyset($1, $2, $3, $4, $5::jsonb)
