SELECT state IN ('CANCELLED', 'DENIED', 'EXPIRED', 'FAILED') OR expires_at <= clock_timestamp()
  FROM integration_gateway.provider_authorization_attempts
 WHERE authorization_id = @authorization_id
   AND lease_id = @lease_id AND lease_generation = @lease_generation
