-- name: run_attachment_image_dependencies :one
SELECT jsonb_build_array(image.ref,image.version,image.policy_revision,image.policy_sha256,
  image.admission_state,image.admission_revision,image.admission_receipt_sha256,
  image.promotion_state,image.promotion_readback_sha256,
  image.role_runtime_contract_revision,image.role_runtime_contract_sha256,
  recipe.ref,recipe.version,recipe.generation,recipe.state)
FROM control_plane.agents agent
LEFT JOIN control_plane.agent_runtime_environment_bindings binding ON binding.agent_id=agent.id
LEFT JOIN control_plane.runtime_environment_versions environment ON environment.id=binding.environment_version_id
LEFT JOIN control_plane.image_artifacts image ON image.id=environment.role_image_artifact_id
LEFT JOIN control_plane.role_image_recipes recipe ON recipe.id=image.recipe_id
WHERE agent.organization_id=@organization_id::uuid AND agent.project_id=@project_id::uuid AND agent.ref=@agent_ref
