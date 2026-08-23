-- name: commands_execute_insert_idempotency_receipts_organization_id_operation_intent_digest :exec
INSERT INTO control_plane.idempotency_receipts(organization_id,actor_id,operation,idempotency_key,intent_digest,response_type,response_payload,expires_at) VALUES($1::uuid,$2::uuid,$3,$4,$5,$6,$7,clock_timestamp()+interval '24 hours')
