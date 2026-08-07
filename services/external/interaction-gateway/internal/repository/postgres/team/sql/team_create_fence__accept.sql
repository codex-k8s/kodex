-- name: team_create_fence__accept :exec
-- params: @arg1,@arg2,@arg3,@arg4
UPDATE interaction_gateway_team_create_fences
SET state = 'BOUND', provider_team_id = @arg4::text, updated_at = clock_timestamp()
WHERE organization_id = @arg1::uuid AND project_id = @arg2::uuid AND operation_id = @arg3::uuid;
