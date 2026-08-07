UPDATE integration_gateway.provider_authorization_attempts
   SET state = 'CODE_ISSUED', version = version + 1,
       provider_login_id_ciphertext = @login_id_ciphertext,
       device_result_ciphertext = @device_result_ciphertext,
       code_expires_at = @code_expires_at, updated_at = @updated_at,
       payload = jsonb_set(jsonb_set(payload, '{state}', '"CODE_ISSUED"'::jsonb),
                           '{version}', to_jsonb(version + 1))
 WHERE authorization_id = @authorization_id AND state = 'PENDING'
   AND lease_id = @lease_id AND lease_generation = @lease_generation
   AND expires_at > clock_timestamp() AND @code_expires_at <= expires_at
RETURNING authorization_id
