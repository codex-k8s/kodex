UPDATE integration_gateway.provider_authorization_attempts
   SET state = @state, version = version + 1, failure_category = @failure_category,
       provider_login_id_ciphertext = ''::bytea, device_result_ciphertext = ''::bytea,
       lease_id = '', lease_expires_at = NULL, updated_at = @updated_at,
       payload = jsonb_set(jsonb_set(jsonb_set(payload, '{state}', to_jsonb(@state::text)),
                                     '{failure_category}', to_jsonb(@failure_category::text)),
                           '{version}', to_jsonb(version + 1))
 WHERE authorization_id = @authorization_id AND state IN ('PENDING', 'CODE_ISSUED')
   AND lease_id = @lease_id AND lease_generation = @lease_generation
RETURNING authorization_id
