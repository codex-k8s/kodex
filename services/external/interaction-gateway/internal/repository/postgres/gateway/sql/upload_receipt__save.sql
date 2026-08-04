INSERT INTO interaction_gateway_upload_receipts (
    delivery_id, artifact_id, organization_id, project_id, provider_file_id,
    channel_id, name, size_bytes, media_type, sha256
)
SELECT delivery.id, $2::uuid, delivery.organization_id, delivery.project_id,
       $3, $4, $5, $6, $7, $8
FROM interaction_gateway_deliveries AS delivery
WHERE delivery.id = $1 AND delivery.fence = $9
  AND delivery.lease_token_sha256 = $10 AND delivery.state = 'DELIVERING'
ON CONFLICT (delivery_id, artifact_id) DO NOTHING;
