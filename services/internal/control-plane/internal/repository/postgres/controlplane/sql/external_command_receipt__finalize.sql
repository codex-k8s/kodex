UPDATE control_plane.external_command_receipt_consumptions
SET result_resource_id = @result_resource_id::uuid,
    result_version = @result_version,
    result_sha256 = @result_sha256,
    result_snapshot = @result_snapshot::jsonb
WHERE issuer = @issuer
  AND purpose = @purpose
  AND receipt_id = @receipt_id::uuid
  AND command_intent_sha256 = @command_intent_sha256
  AND result_resource_id IS NULL
  AND result_version = 0
  AND result_sha256 IS NULL
  AND result_snapshot IS NULL
