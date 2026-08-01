-- name: RuntimeRevisionComponents
SELECT
    id::text,
    organization_id::text,
    project_id::text,
    coalesce(parent_id::text, ''),
    owner_actor_id::text,
    kind,
    name,
    state,
    version,
    spec,
    created_at,
    updated_at
FROM control_plane.resources
WHERE organization_id = @organization_id::uuid
  AND project_id = @project_id::uuid
  AND state = 'ACTIVE'
  AND kind IN (
      'PROJECT',
      'CHAT',
      'ROLE',
      'PROMPT_PROFILE',
      'CREDENTIAL_BINDING',
      'REPOSITORY_WORKSPACE',
      'INTEGRATION'
      ,'SESSION'
  )
ORDER BY kind, id
FOR SHARE
