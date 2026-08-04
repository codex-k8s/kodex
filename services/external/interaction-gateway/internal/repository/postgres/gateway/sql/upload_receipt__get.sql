SELECT delivery_id, artifact_id, provider_file_id, channel_id, name,
       size_bytes, media_type, sha256, created_at
FROM interaction_gateway_upload_receipts
WHERE delivery_id = $1 AND artifact_id = $2;
