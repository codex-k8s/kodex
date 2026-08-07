-- name: team_create_fence__acquire :exec
-- params: @arg1,@arg2,@arg3,@arg4
INSERT INTO interaction_gateway_team_create_fences(
    organization_id, project_id, operation_id, request_sha256, state
) VALUES (@arg1::uuid, @arg2::uuid, @arg3::uuid, @arg4::text, 'ACTIVE')
ON CONFLICT (organization_id, project_id) DO NOTHING;
