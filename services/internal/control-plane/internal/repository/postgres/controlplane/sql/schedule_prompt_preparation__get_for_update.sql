SELECT organization_id::text, project_id::text, owner_actor_id::text,
    key_hash, request_sha256, semantic_sha256, action,
    coalesce(target_id::text, ''), expected_version, object_key, state,
    generation, lease_expires_at, coalesce(object_reference, ''),
    coalesce(object_version_id, ''), coalesce(object_sha256, ''),
    coalesce(object_size, 0), coalesce(object_media_type, ''),
    created_at, updated_at
FROM control_plane.schedule_prompt_preparations
WHERE organization_id = @organization_id::uuid
  AND project_id = @project_id::uuid
  AND owner_actor_id = @owner_actor_id::uuid
  AND key_hash = @key_hash
FOR UPDATE;
