INSERT INTO interaction_gateway_deliveries
    (id, kind, state, organization_id, project_id, session_id, turn_id, attempt,
     immutable_input_sha256, team_id, channel_id, root_post_id, bot_stable_key,
     locale, payload, payload_sha256, attachments, next_attempt_at,
     owner_gate_id, owner_gate_version, process_run_id, process_version,
     owner_gate_claim_token_ciphertext, owner_gate_claim_fence,
     owner_gate_claim_expires_at, recipient_actor_id, owner_gate_payload_sha256)
VALUES ($1, $2, 'PENDING', $3, $4, NULLIF($5, '')::uuid, NULLIF($6, '')::uuid,
        NULLIF($7, 0), $8, $9, $10, $11, $12, $13, $14, $15, $16, now(),
        NULLIF($17, '')::uuid, NULLIF($18, 0), NULLIF($19, '')::uuid,
        NULLIF($20, 0), $21, NULLIF($22, 0), $23, NULLIF($24, '')::uuid, $25)
ON CONFLICT (id) DO NOTHING;
