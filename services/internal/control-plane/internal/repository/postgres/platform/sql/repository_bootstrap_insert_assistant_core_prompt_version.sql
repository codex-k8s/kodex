-- name: repository_bootstrap_insert_assistant_core_prompt_version :one
INSERT INTO control_plane.instruction_versions(
    ref,
    organization_id,
    agent_id,
    version_number,
    state,
    content,
    digest,
    core,
    published_at
)
SELECT @prompt_ref,
       @organization_id::uuid,
       @agent_id::uuid,
       COALESCE(MAX(existing.version_number), 0) + 1,
       'PUBLISHED',
       @content,
       @digest,
       true,
       clock_timestamp()
FROM control_plane.instruction_versions existing
WHERE existing.organization_id = @organization_id::uuid
  AND existing.agent_id = @agent_id::uuid
RETURNING ref;
