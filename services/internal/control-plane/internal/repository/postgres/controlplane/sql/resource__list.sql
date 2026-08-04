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
  AND kind = @kind
  AND state <> 'DELETED'
  AND (
      project_id = nullif(@project_id, '')::uuid
      OR (@project_id = '' AND kind = 'PROJECT')
  )
  AND (@parent_id = '' OR parent_id = @parent_id::uuid)
  AND (cardinality(@states::text[]) = 0 OR state = ANY(@states::text[]))
  AND (@after_id = '' OR id > @after_id::uuid)
  AND (
      kind NOT IN ('SESSION', 'TURN', 'PROCESS_RUN', 'SCHEDULE', 'OWNER_GATE', 'WORK_CLAIM')
      OR owner_actor_id = @actor_id::uuid
  )
  AND (
      kind <> 'WORK_CLAIM'
      OR state <> 'ACTIVE'
      OR control_plane.work_claim_graph_is_active(resources)
  )
ORDER BY id
LIMIT @limit
