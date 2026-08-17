-- name: mattermost_runtime_checkpoint__admission :one
-- params: @arg1,@arg2
SELECT mapping_id::text, mapping_version, mapping_generation, mapping_state,
       mapping_digest_sha256
FROM interaction_gateway_mattermost_runtime_checkpoints
WHERE organization_id = @arg1::uuid
  AND project_id = @arg2::uuid
  AND mapping_state = 'BOUND';
