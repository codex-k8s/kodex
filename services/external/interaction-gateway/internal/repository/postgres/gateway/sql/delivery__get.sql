SELECT id, kind, state, organization_id, project_id, session_id, turn_id,
       COALESCE(attempt, 0), immutable_input_sha256, team_id, channel_id,
       root_post_id, bot_stable_key, locale, payload, payload_sha256,
       attachments, provider_post_id, provider_receipt_sha256, update_post_id, attempts, ack_attempts, fence,
       lease_expires_at, next_attempt_at, last_error_code, owner_gate_id,
       COALESCE(owner_gate_version, 0), process_run_id, COALESCE(process_version, 0),
       owner_gate_claim_token_ciphertext, COALESCE(owner_gate_claim_fence, 0),
       owner_gate_claim_expires_at, recipient_actor_id, owner_gate_payload_sha256, delivery_recorded_at,
       created_at, updated_at, COALESCE(owner_delivery_fence, 0), owner_delivery_token_ciphertext,
       owner_delivery_expires_at, COALESCE(owner_turn_version, 0), owner_runtime_revision_id,
       COALESCE(owner_runtime_revision_version, 0), owner_delivery_recorded_at
FROM interaction_gateway_deliveries WHERE id = $1;
