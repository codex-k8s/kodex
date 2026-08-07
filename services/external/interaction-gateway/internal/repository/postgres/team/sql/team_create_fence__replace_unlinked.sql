-- name: team_create_fence__replace_unlinked :exec
-- params: @arg1,@arg2,@arg3,@arg4
UPDATE interaction_gateway_team_create_fences
SET operation_id = @arg3::uuid, request_sha256 = @arg4::text, state = 'ACTIVE',
    provider_team_id = '', updated_at = clock_timestamp()
WHERE organization_id = @arg1::uuid AND project_id = @arg2::uuid AND state = 'UNLINKED';
