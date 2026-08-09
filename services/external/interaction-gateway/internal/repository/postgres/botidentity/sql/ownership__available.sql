-- name: ownership__available :one
-- params: @arg1,@arg2,@arg3
SELECT NOT EXISTS (
    SELECT 1 FROM interaction_gateway_agent_bot_ownership AS ownership
    WHERE ownership.organization_id = @arg1::uuid AND ownership.project_id = @arg2::uuid
      AND ownership.provider_object_ref = @arg3::uuid
);
