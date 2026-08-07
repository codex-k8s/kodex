-- name: mattermost_runtime_route__delete_project :exec
-- params: @arg1,@arg2
DELETE FROM interaction_gateway_mattermost_runtime_routes
WHERE organization_id = @arg1::uuid AND project_id = @arg2::uuid;
