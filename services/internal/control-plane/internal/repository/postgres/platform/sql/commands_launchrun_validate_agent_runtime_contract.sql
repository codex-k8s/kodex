-- name: commands_launchrun_validate_agent_runtime_contract :one
WITH requested(ref) AS (
    SELECT DISTINCT unnest(@agent_refs::text[])
), eligible AS (
    SELECT DISTINCT agent.ref
    FROM requested
    JOIN control_plane.agents agent
      ON agent.ref = requested.ref
     AND agent.organization_id = @organization_id::uuid
     AND agent.project_id = @project_id::uuid
     AND agent.enabled
     AND agent.state = 'READY'
    JOIN control_plane.agent_runtime_environment_bindings binding
      ON binding.agent_id = agent.id
    JOIN control_plane.runtime_environment_sets environment
      ON environment.id = binding.environment_set_id
     AND environment.state = 'ACTIVE'
    JOIN control_plane.runtime_environment_versions environment_version
      ON environment_version.id = binding.environment_version_id
    JOIN control_plane.image_artifacts artifact
      ON artifact.id = environment_version.role_image_artifact_id
     AND artifact.organization_id = agent.organization_id
     AND artifact.admission_state = 'ACCEPTED'
     AND artifact.promotion_state = 'PROMOTED'
     AND artifact.promoted_reference <> ''
     AND artifact.role_runtime_contract_revision = @role_runtime_contract_revision
     AND artifact.role_runtime_contract_sha256 = @role_runtime_contract_sha256
    JOIN control_plane.role_image_recipes recipe
      ON recipe.id = artifact.recipe_id
     AND recipe.project_id = agent.project_id
     AND recipe.state = 'ACTIVE'
)
SELECT (SELECT count(*) FROM requested) > 0
   AND (SELECT count(*) FROM eligible) = (SELECT count(*) FROM requested);
