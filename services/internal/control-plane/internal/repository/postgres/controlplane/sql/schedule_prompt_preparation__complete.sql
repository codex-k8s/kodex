UPDATE control_plane.schedule_prompt_preparations
SET state = 'READY',
    lease_expires_at = NULL,
    object_reference = @object_reference,
    object_version_id = @object_version_id,
    object_sha256 = @object_sha256,
    object_size = @object_size,
    object_media_type = @object_media_type,
    updated_at = @updated_at
WHERE organization_id = @organization_id::uuid
  AND project_id = @project_id::uuid
  AND owner_actor_id = @owner_actor_id::uuid
  AND key_hash = @key_hash
  AND request_sha256 = @request_sha256
  AND generation = @generation
  AND state = 'WRITING';
