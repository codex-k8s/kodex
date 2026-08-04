UPDATE interaction_gateway_deliveries
   SET owner_delivery_fence = $2,
       owner_delivery_token_ciphertext = $3,
       owner_delivery_expires_at = $4,
       updated_at = clock_timestamp()
 WHERE id = $1
   AND owner_delivery_recorded_at IS NULL
   AND state IN ('PENDING', 'DELIVERING', 'PROVIDER_ACCEPTED')
   AND owner_delivery_fence < $2
   AND organization_id = $5::uuid
   AND project_id = $6::uuid
   AND turn_id = $7::uuid
   AND owner_turn_version = $8
   AND owner_runtime_revision_id = $9::uuid
   AND owner_runtime_revision_version = $10
   AND payload_sha256 = $11;
