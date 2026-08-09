INSERT INTO control_plane.schedule_prompt_preparations (
    organization_id, project_id, owner_actor_id, key_hash,
    request_sha256, semantic_sha256, action, target_id, expected_version,
    object_key, state, generation, lease_expires_at, created_at, updated_at
) VALUES (
    @organization_id::uuid, @project_id::uuid, @owner_actor_id::uuid,
    @key_hash, @request_sha256, @semantic_sha256, @action,
    nullif(@target_id, '')::uuid, @expected_version, @object_key,
    'WRITING', 1, @lease_expires_at, @created_at, @created_at
)
ON CONFLICT (organization_id, project_id, owner_actor_id, key_hash)
DO NOTHING;
