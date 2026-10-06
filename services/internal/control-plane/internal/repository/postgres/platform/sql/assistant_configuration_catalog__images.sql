-- name: assistant_configuration_catalog__images :many
SELECT CASE WHEN @artifacts THEN artifact.ref ELSE recipe.ref END,recipe.name,
       CASE WHEN @artifacts THEN artifact.version ELSE recipe.version END,recipe.generation,
       CASE WHEN @artifacts THEN artifact.promoted_reference ELSE '' END,
       CASE WHEN @artifacts THEN artifact.manifest_digest ELSE '' END,
       CASE WHEN @artifacts THEN '' ELSE recipe.specification->>'EnvironmentKey' END
FROM control_plane.role_image_recipes recipe
LEFT JOIN control_plane.projects project ON project.id=recipe.project_id AND project.organization_id=recipe.organization_id
LEFT JOIN control_plane.image_artifacts artifact ON artifact.id=recipe.active_image_artifact_id
  AND artifact.organization_id=recipe.organization_id AND artifact.recipe_id=recipe.id
WHERE recipe.organization_id=@organization_id::uuid AND recipe.scope_kind=@scope_kind AND recipe.state='ACTIVE'
  AND ((@scope_kind='ORGANIZATION' AND recipe.project_id IS NULL
        AND control_plane.organization_role_image_actor_allowed(recipe.organization_id,@actor_id::uuid))
    OR (@scope_kind='PROJECT' AND project.ref=@project_ref AND project.lifecycle='ACTIVE'
        AND control_plane.catalog_resource_visible(recipe.organization_id,@actor_id::uuid,'image.source.view','ROLE_IMAGE',recipe.id,recipe.project_id,recipe.created_by,jsonb_build_object('PROJECT',recipe.project_id::text),transaction_timestamp())))
  AND (@authority_project='' OR recipe.project_id=NULLIF(@authority_project,'')::uuid)
  AND (@query='' OR strpos(lower(recipe.name),lower(@query))>0 OR strpos(lower(recipe.ref),lower(@query))>0)
  AND (NOT @artifacts OR (artifact.admission_verdict='ACCEPTED' AND artifact.promotion_state='PROMOTED'
       AND artifact.promoted_reference<>'' AND artifact.promotion_readback_sha256<>''
       AND artifact.recipe_generation=recipe.generation AND artifact.spec_sha256=recipe.spec_sha256
       AND artifact.scope_kind=recipe.scope_kind AND artifact.project_id IS NOT DISTINCT FROM recipe.project_id))
ORDER BY recipe.name,recipe.ref LIMIT 11 OFFSET @offset;
