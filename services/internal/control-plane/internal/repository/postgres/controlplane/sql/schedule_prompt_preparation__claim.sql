UPDATE control_plane.schedule_prompt_preparations
SET state = 'WRITING',
    generation = generation + 1,
    lease_expires_at = @lease_expires_at,
    object_reference = NULL,
    object_version_id = NULL,
    object_sha256 = NULL,
    object_size = NULL,
    object_media_type = NULL,
    updated_at = @updated_at
WHERE organization_id = @organization_id::uuid
  AND project_id = @project_id::uuid
  AND owner_actor_id = @owner_actor_id::uuid
  AND key_hash = @key_hash
  AND generation = @expected_generation
  AND state IN ('WRITING', 'AMBIGUOUS');
