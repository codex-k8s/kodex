UPDATE interaction_gateway_download_grants
   SET revoked_at = COALESCE(revoked_at, clock_timestamp()),
       updated_at = clock_timestamp()
 WHERE organization_id = $1::uuid
   AND project_id = $2::uuid
   AND channel_id = $3
   AND ($4 = '' OR session_id = $4::uuid)
   AND consumed_at IS NULL;
