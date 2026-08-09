-- name: ownership__reserve_operation :one
-- params: @arg1,@arg2,@arg3,@arg4,@arg5,@arg6,@arg7
WITH active_operation AS (
    SELECT 1
    FROM interaction_gateway_agent_bot_operations
    WHERE operation_id = @arg1::uuid AND organization_id = @arg2::uuid
      AND project_id = @arg3::uuid AND agent_ref = @arg5::uuid
      AND fence = @arg6::bigint AND lease_token_sha256 = @arg7::text
      AND lease_expires_at > clock_timestamp()
      AND state NOT IN ('BOUND', 'REVOKED', 'REPAIR_REQUIRED')
), reserved AS (
    INSERT INTO interaction_gateway_agent_bot_ownership (
        organization_id, project_id, provider_object_ref, agent_ref
    )
    SELECT @arg2::uuid, @arg3::uuid, @arg4::uuid, @arg5::uuid
    FROM active_operation
    ON CONFLICT (organization_id, project_id, provider_object_ref) DO UPDATE
    SET updated_at = clock_timestamp()
    WHERE interaction_gateway_agent_bot_ownership.agent_ref = EXCLUDED.agent_ref
    RETURNING agent_ref
)
SELECT agent_ref::text FROM reserved;
