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
  AND lower(name) LIKE
      '%' || replace(replace(replace(lower(@query), '\', '\\'), '%', '\%'), '_', '\_') || '%'
      ESCAPE '\'
ORDER BY id
LIMIT @limit
