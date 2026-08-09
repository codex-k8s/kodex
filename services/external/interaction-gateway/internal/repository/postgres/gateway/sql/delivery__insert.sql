INSERT INTO interaction_gateway_deliveries
    (id, kind, state, organization_id, project_id, session_id, turn_id, attempt,
     immutable_input_sha256, team_id, channel_id, root_post_id, bot_stable_key,
     locale, payload, payload_sha256, attachments, update_post_id, next_attempt_at,
     owner_gate_id, owner_gate_version, process_run_id, process_version,
     owner_gate_claim_token_ciphertext, owner_gate_claim_fence,
     owner_gate_claim_expires_at, recipient_actor_id, owner_gate_payload_sha256,
     owner_delivery_fence, owner_delivery_token_ciphertext, owner_delivery_expires_at,
     owner_turn_version, owner_runtime_revision_id, owner_runtime_revision_version,
     bot_provider_user_id, bot_provider_generation)
VALUES ($1, $2, 'PENDING', $3, $4, NULLIF($5, '')::uuid, NULLIF($6, '')::uuid,
        NULLIF($7, 0), $8, $9, $10, $11, $12, $13, $14, $15, $16, $26, clock_timestamp(),
        NULLIF($17, '')::uuid, NULLIF($18, 0), NULLIF($19, '')::uuid,
        NULLIF($20, 0), $21, NULLIF($22, 0), $23, NULLIF($24, '')::uuid, $25,
        NULLIF($27, 0), $28, $29, NULLIF($30, 0), NULLIF($31, '')::uuid, NULLIF($32, 0),
        $33, $34)
ON CONFLICT (id) DO NOTHING;
