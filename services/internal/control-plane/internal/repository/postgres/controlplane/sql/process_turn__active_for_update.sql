-- name: ProcessTurnActiveForUpdate
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
  AND kind = 'TURN'
  AND spec ->> 'processRunId' = @process_run_id
  AND state IN (
      'QUEUED', 'CLAIMED', 'RUNNING', 'WAITING_OWNER',
      'WAITING_EXTERNAL', 'BLOCKED'
  )
ORDER BY id
FOR UPDATE
