-- name: missioncontrol__update_command_status :one
UPDATE mission_control_commands
SET
    status = $2,
    failure_reason = $3,
    approval_request_id = $4,
    approval_state = $5,
    approval_requested_at = $6,
    approval_decided_at = $7,
    result_payload = $8,
    provider_delivery_ids = $9,
    updated_at = COALESCE($10, NOW()),
    reconciled_at = $11
WHERE id = $1
RETURNING
    id::text AS id,
    project_id::text AS project_id,
    command_kind,
    target_entity_id,
    actor_id,
    business_intent_key,
    correlation_id,
    status,
    failure_reason,
    approval_request_id::text AS approval_request_id,
    approval_state,
    approval_requested_at,
    approval_decided_at,
    payload AS payload_json,
    result_payload AS result_payload_json,
    provider_delivery_ids AS provider_deliveries_json,
    requested_at,
    updated_at,
    reconciled_at;
