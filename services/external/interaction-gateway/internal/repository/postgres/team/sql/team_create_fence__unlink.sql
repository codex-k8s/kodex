-- name: team_create_fence__unlink :exec
-- params: @arg1,@arg2,@arg3
UPDATE interaction_gateway_team_create_fences
SET state = 'UNLINKED', updated_at = clock_timestamp()
WHERE organization_id = @arg1::uuid AND project_id = @arg2::uuid AND provider_team_id = @arg3::text;
