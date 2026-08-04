-- name: ResourceSearch
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
  AND kind = @kind
  AND state <> 'DELETED'
  AND (cardinality(@states::text[]) = 0 OR state = ANY(@states::text[]))
  AND id > coalesce(
      nullif(@after_id, '')::uuid,
      '00000000-0000-0000-0000-000000000000'::uuid
  )
  AND (
      kind NOT IN ('SESSION', 'TURN', 'PROCESS_RUN', 'SCHEDULE', 'OWNER_GATE', 'WORK_CLAIM')
      OR owner_actor_id = @actor_id::uuid
  )
  AND (
      kind <> 'WORK_CLAIM'
      OR state <> 'ACTIVE'
      OR control_plane.work_claim_graph_is_active(resources)
  )
  AND lower(name) LIKE
      '%' || replace(replace(replace(lower(@query), '\', '\\'), '%', '\%'), '_', '\_') || '%'
      ESCAPE '\'
ORDER BY id
LIMIT @limit
