-- name: ScheduleSessionConversationForUpdate
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
  AND kind = 'SESSION'
  AND state IN (
      'ACTIVE', 'PAUSED', 'QUEUED', 'CLAIMED', 'RUNNING',
      'WAITING_EXTERNAL', 'WAITING_OWNER', 'BLOCKED'
  )
  AND coalesce(spec ->> 'conversationId', '') = @conversation_id
ORDER BY id
FOR UPDATE;
