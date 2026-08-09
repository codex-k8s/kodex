UPDATE control_plane.schedule_prompt_preparations
SET state = 'CONSUMED',
    result_schedule_id = @schedule_id::uuid,
    result_schedule_version = @schedule_version,
    updated_at = @updated_at
WHERE organization_id = @organization_id::uuid
  AND project_id = @project_id::uuid
  AND owner_actor_id = @owner_actor_id::uuid
  AND key_hash = @key_hash
  AND generation = @generation
  AND state IN ('READY', 'CONSUMED')
  AND (result_schedule_id IS NULL OR result_schedule_id = @schedule_id::uuid)
  AND (result_schedule_version IS NULL OR result_schedule_version = @schedule_version);
