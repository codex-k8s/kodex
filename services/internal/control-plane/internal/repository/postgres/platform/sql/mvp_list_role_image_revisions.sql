-- name: mvp_list_role_image_revisions :many
SELECT revision.ref, recipe.ref, revision.revision, revision.recipe_version,
       revision.recipe_generation, revision.spec_sha256, COALESCE(artifact.ref, ''),
       revision.provenance_sha256, revision.source_sha256, revision.immutable_build_sha256,
       revision.manifest_digest, revision.promoted_reference, revision.promotion_receipt_sha256,
       revision.created_at
FROM control_plane.role_image_recipe_revisions revision
JOIN control_plane.role_image_recipes recipe ON recipe.id = revision.recipe_id
LEFT JOIN control_plane.image_artifacts artifact ON artifact.id = revision.image_artifact_id
WHERE revision.organization_id = @organization_id::uuid
  AND recipe.ref = @recipe_ref
  AND (@before_revision = 0 OR revision.revision < @before_revision)
ORDER BY revision.revision DESC
LIMIT @page_size;
