-- name: follow_up_intent__reserve_dispatch :exec
UPDATE agent_manager_follow_up_intents
SET
    provider_operation_ref = @provider_operation_ref,
    version = @version,
    updated_at = @updated_at
WHERE id = @id
  AND version = @previous_version
  AND status IN ('planned', 'requested');
