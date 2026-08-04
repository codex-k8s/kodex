UPDATE interaction_gateway_download_grants
SET consumed_at = clock_timestamp(), authenticated_at = clock_timestamp(),
    authenticated_user_id = $3, updated_at = clock_timestamp()
WHERE id = $1::uuid AND generation = $2
  AND consumed_at IS NULL AND revoked_at IS NULL AND expires_at > clock_timestamp()
RETURNING consumed_at, authenticated_at;
