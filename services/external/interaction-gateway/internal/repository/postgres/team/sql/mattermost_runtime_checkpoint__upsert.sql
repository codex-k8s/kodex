-- name: mattermost_runtime_checkpoint__upsert :exec
-- params: @arg1,@arg2,@arg3,@arg4,@arg5,@arg6,@arg7
INSERT INTO interaction_gateway_mattermost_runtime_checkpoints(
    organization_id, project_id, mapping_id, mapping_version,
    mapping_generation, mapping_state, mapping_digest_sha256
) VALUES (@arg1::uuid, @arg2::uuid, @arg3::uuid, @arg4::bigint,
    @arg5::bigint, @arg6::text, @arg7::text)
ON CONFLICT (organization_id, project_id) DO UPDATE
SET mapping_id = EXCLUDED.mapping_id,
    mapping_version = EXCLUDED.mapping_version,
    mapping_generation = EXCLUDED.mapping_generation,
    mapping_state = EXCLUDED.mapping_state,
    mapping_digest_sha256 = EXCLUDED.mapping_digest_sha256,
    updated_at = clock_timestamp()
WHERE interaction_gateway_mattermost_runtime_checkpoints.mapping_generation < EXCLUDED.mapping_generation
   OR (interaction_gateway_mattermost_runtime_checkpoints.mapping_generation = EXCLUDED.mapping_generation
       AND interaction_gateway_mattermost_runtime_checkpoints.mapping_digest_sha256 = EXCLUDED.mapping_digest_sha256);
