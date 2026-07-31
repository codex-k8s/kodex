-- name: WorkClaimActiveForUpdate
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
  AND kind = 'WORK_CLAIM'
  AND state = 'ACTIVE'
  AND (@process_run_id = '' OR spec ->> 'processRunId' = @process_run_id)
  AND (@turn_id = '' OR spec ->> 'turnId' = @turn_id)
ORDER BY id
FOR UPDATE
