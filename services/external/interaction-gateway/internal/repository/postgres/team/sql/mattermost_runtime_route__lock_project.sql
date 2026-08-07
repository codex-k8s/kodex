-- name: mattermost_runtime_route__lock_project :one
-- params: @arg1,@arg2
SELECT mapping_generation, mapping_digest_sha256
FROM interaction_gateway_mattermost_runtime_checkpoints
WHERE organization_id = @arg1::uuid AND project_id = @arg2::uuid
FOR UPDATE;
