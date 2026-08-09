UPDATE control_plane.schedule_prompt_preparations
SET state = 'AMBIGUOUS',
    lease_expires_at = NULL,
    updated_at = @updated_at
WHERE organization_id = @organization_id::uuid
  AND project_id = @project_id::uuid
  AND owner_actor_id = @owner_actor_id::uuid
  AND key_hash = @key_hash
  AND request_sha256 = @request_sha256
  AND generation = @generation
  AND state = 'WRITING';
