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
  AND owner_actor_id = @actor_id::uuid
  AND kind = @kind
  AND state <> 'DELETED'
  AND (@parent_id = '' OR parent_id = @parent_id::uuid)
  AND (
      @backup_id = ''
      OR (kind = 'WORKSPACE_RESTORE' AND spec ->> 'backupId' = @backup_id)
  )
  AND (cardinality(@states::text[]) = 0 OR state = ANY(@states::text[]))
  AND (@after_id = '' OR id > @after_id::uuid)
ORDER BY id
LIMIT @limit;
