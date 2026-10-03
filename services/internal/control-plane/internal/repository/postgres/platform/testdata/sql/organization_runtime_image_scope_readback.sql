-- name: organization_runtime_image_scope_readback :one
SELECT assistant.organization_id::text,
       artifact.id::text,
       artifact.ref,
       recipe.scope_kind,
       recipe.project_id IS NULL AND artifact.project_id IS NULL AND environment.project_id IS NULL,
       environment.current_version_id::text,
       (SELECT count(*) FROM control_plane.runtime_environment_versions history
        WHERE history.environment_set_id = environment.id),
       project_image.project_id::text,
       project_image.ref
FROM control_plane.assistant_runtime assistant
JOIN control_plane.agent_runtime_environment_bindings binding ON binding.agent_id = assistant.agent_id
JOIN control_plane.runtime_environment_sets environment ON environment.id = binding.environment_set_id
JOIN control_plane.runtime_environment_versions version ON version.id = environment.current_version_id
JOIN control_plane.image_artifacts artifact ON artifact.id = version.role_image_artifact_id
JOIN control_plane.role_image_recipes recipe ON recipe.id = artifact.recipe_id
CROSS JOIN LATERAL (
    SELECT candidate.project_id, candidate.ref
    FROM control_plane.image_artifacts candidate
    JOIN control_plane.role_image_recipes candidate_recipe ON candidate_recipe.id = candidate.recipe_id
    WHERE candidate.organization_id = assistant.organization_id
      AND candidate.project_id IS NOT NULL
      AND candidate.admission_state = 'ACCEPTED' AND candidate.promotion_state = 'PROMOTED'
      AND candidate_recipe.state = 'ACTIVE'
    ORDER BY candidate.ref
    LIMIT 1
) project_image
WHERE assistant.stable_key = 'system-assistant';
