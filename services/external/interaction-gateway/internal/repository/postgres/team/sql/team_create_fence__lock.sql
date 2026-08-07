-- name: team_create_fence__lock :one
-- params: @arg1,@arg2
SELECT operation_id::text, request_sha256, state, provider_team_id
FROM interaction_gateway_team_create_fences
WHERE organization_id = @arg1::uuid AND project_id = @arg2::uuid
FOR UPDATE;
