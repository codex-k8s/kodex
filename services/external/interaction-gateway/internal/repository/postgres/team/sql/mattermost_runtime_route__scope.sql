-- name: mattermost_runtime_route__scope :one
-- params: @arg1,@arg2
SELECT organization_id::text, project_id::text
FROM interaction_gateway_mattermost_runtime_route_scope(@arg1::text, @arg2::text);
