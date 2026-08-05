SELECT
    id::text, organization_id::text, project_id::text,
    coalesce(parent_id::text, ''), owner_actor_id::text,
    kind, name, state, version, spec, created_at, updated_at
FROM control_plane.resources
WHERE organization_id = @organization_id::uuid
  AND project_id = @project_id::uuid
  AND kind = 'IMAGE_ARTIFACT'
  AND spec ->> 'recipeId' = @recipe_id
  AND state = 'WAITING_EXTERNAL'
ORDER BY created_at, id
FOR UPDATE
