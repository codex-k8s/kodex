-- name: provider_verification_cancel_previous_catalog :exec
UPDATE control_plane.provider_model_catalog_tasks
SET state = 'CANCELLED', claimant_id = '', fence = '', request_digest = '', expires_at = NULL, completed_at = clock_timestamp()
WHERE provider_account_id = @account_id::uuid AND organization_id = @organization_id::uuid
  AND state IN ('PENDING', 'CLAIMED');
