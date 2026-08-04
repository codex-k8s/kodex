UPDATE control_plane.interaction_delivery_work
SET state = 'DELIVERED', provider_receipt_sha256 = @provider_receipt_sha256,
    delivered_at = clock_timestamp(), lease_owner = '', lease_token_sha256 = '',
    lease_expires_at = NULL, updated_at = clock_timestamp()
WHERE id = @id AND organization_id = @organization_id AND project_id = @project_id
  AND state = 'CLAIMED' AND fence = @fence AND lease_token_sha256 = @lease_token_sha256
  AND lease_expires_at > clock_timestamp();
