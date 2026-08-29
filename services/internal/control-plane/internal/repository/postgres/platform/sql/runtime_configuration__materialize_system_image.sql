-- name: runtime_configuration__materialize_system_image :one
WITH current_agent AS (
    SELECT agent.role_definition_id
    FROM control_plane.agents agent
    WHERE agent.organization_id = @organization_id::uuid
      AND agent.project_id = @project_id::uuid
      AND agent.id = @agent_id::uuid
), changed_recipe AS (
    INSERT INTO control_plane.role_image_recipes
        (ref, organization_id, project_id, role_definition_id, name, state, specification,
         generation, spec_sha256, policy_revision, policy_sha256,
         role_runtime_contract_revision, role_runtime_contract_sha256, created_by)
    SELECT @recipe_ref, @organization_id::uuid, @project_id::uuid,
           current_agent.role_definition_id, 'i18n:SYSTEM_BASE_ROLE_IMAGE', 'ACTIVE',
           @specification, 1, @spec_sha256, @policy_revision, @policy_sha256,
           @contract_revision, @contract_sha256, @created_by::uuid
    FROM current_agent
    ON CONFLICT (role_definition_id, name) DO UPDATE
    SET state = 'ACTIVE',
        specification = EXCLUDED.specification,
        generation = control_plane.role_image_recipes.generation + 1,
        spec_sha256 = EXCLUDED.spec_sha256,
        policy_revision = EXCLUDED.policy_revision,
        policy_sha256 = EXCLUDED.policy_sha256,
        role_runtime_contract_revision = EXCLUDED.role_runtime_contract_revision,
        role_runtime_contract_sha256 = EXCLUDED.role_runtime_contract_sha256,
        version = control_plane.role_image_recipes.version + 1,
        updated_at = clock_timestamp()
    WHERE control_plane.role_image_recipes.spec_sha256 <> EXCLUDED.spec_sha256
       OR control_plane.role_image_recipes.policy_revision <> EXCLUDED.policy_revision
       OR control_plane.role_image_recipes.policy_sha256 <> EXCLUDED.policy_sha256
       OR control_plane.role_image_recipes.role_runtime_contract_revision <> EXCLUDED.role_runtime_contract_revision
       OR control_plane.role_image_recipes.role_runtime_contract_sha256 <> EXCLUDED.role_runtime_contract_sha256
       OR control_plane.role_image_recipes.state <> 'ACTIVE'
    RETURNING id, ref, project_id, version, generation, specification, spec_sha256,
              policy_revision, policy_sha256, role_runtime_contract_revision,
              role_runtime_contract_sha256
), recipe AS (
    SELECT * FROM changed_recipe
    UNION ALL
    SELECT existing.id, existing.ref, existing.project_id, existing.version,
           existing.generation, existing.specification, existing.spec_sha256,
           existing.policy_revision, existing.policy_sha256,
           existing.role_runtime_contract_revision,
           existing.role_runtime_contract_sha256
    FROM control_plane.role_image_recipes existing
    JOIN current_agent ON current_agent.role_definition_id = existing.role_definition_id
    WHERE existing.organization_id = @organization_id::uuid
      AND existing.project_id = @project_id::uuid
      AND existing.name = 'i18n:SYSTEM_BASE_ROLE_IMAGE'
      AND existing.state = 'ACTIVE'
      AND NOT EXISTS (SELECT 1 FROM changed_recipe)
    LIMIT 1
), existing_artifact AS (
    SELECT artifact.id, artifact.ref, artifact.recipe_id, artifact.recipe_generation,
           artifact.promoted_reference, artifact.manifest_digest
    FROM control_plane.image_artifacts artifact
    JOIN recipe ON recipe.id = artifact.recipe_id
    WHERE artifact.organization_id = @organization_id::uuid
      AND artifact.project_id = @project_id::uuid
      AND artifact.recipe_generation = recipe.generation
      AND artifact.manifest_digest = @manifest_digest
      AND artifact.promoted_reference = @image_reference
      AND artifact.admission_state = 'ACCEPTED'
      AND artifact.promotion_state = 'PROMOTED'
    ORDER BY artifact.created_at DESC
    LIMIT 1
), inserted_build AS (
    INSERT INTO control_plane.image_builds
        (ref, organization_id, project_id, recipe_id, recipe_version, recipe_generation,
         specification, spec_sha256, immutable_build_sha256, attempt, maximum_attempts,
         stage, progress_percent, staging_reference, manifest_digest, provenance_sha256)
    SELECT @build_ref, @organization_id::uuid, recipe.project_id, recipe.id,
           recipe.version, recipe.generation, recipe.specification, recipe.spec_sha256,
           @immutable_build_sha256, 1, 1, 'COMPLETED', 100, @image_reference,
           @manifest_digest, @provenance_sha256
    FROM recipe
    WHERE NOT EXISTS (SELECT 1 FROM existing_artifact)
    RETURNING id, recipe_id, recipe_version, recipe_generation, specification,
              spec_sha256, immutable_build_sha256, version, attempt,
              staging_reference, manifest_digest, provenance_sha256
), inserted_artifact AS (
    INSERT INTO control_plane.image_artifacts
        (ref, organization_id, project_id, recipe_id, recipe_version, recipe_generation,
         spec_sha256, build_id, build_version, build_attempt, specification,
         policy_revision, policy_sha256, role_runtime_contract_revision,
         role_runtime_contract_sha256, staging_reference, manifest_digest,
         immutable_build_sha256, provenance_sha256, admission_state, sbom_sha256,
         vulnerability_evidence_sha256, admission_verdict, signature_identity,
         signature_sha256, admission_revision, admission_receipt_sha256,
         admission_receipt_oci_manifest_digest, promotion_state, promoted_reference,
         promotion_readback_sha256, promoted_at)
    SELECT @artifact_ref, @organization_id::uuid, recipe.project_id, recipe.id,
           inserted_build.recipe_version, inserted_build.recipe_generation,
           inserted_build.spec_sha256, inserted_build.id, inserted_build.version,
           inserted_build.attempt, inserted_build.specification,
           recipe.policy_revision, recipe.policy_sha256,
           recipe.role_runtime_contract_revision, recipe.role_runtime_contract_sha256,
           inserted_build.staging_reference, inserted_build.manifest_digest,
           inserted_build.immutable_build_sha256, inserted_build.provenance_sha256,
           'ACCEPTED', @evidence_sha256, @evidence_sha256, 'ACCEPTED',
           'platform-owned-bootstrap', @evidence_sha256, 1, @evidence_sha256,
           inserted_build.manifest_digest, 'PROMOTED', @image_reference,
           @evidence_sha256, clock_timestamp()
    FROM inserted_build
    JOIN recipe ON recipe.id = inserted_build.recipe_id
    RETURNING id, ref, recipe_id, recipe_generation, promoted_reference, manifest_digest
), artifact AS (
    SELECT * FROM inserted_artifact
    UNION ALL
    SELECT * FROM existing_artifact
    LIMIT 1
), activated AS (
    UPDATE control_plane.role_image_recipes active_recipe
    SET active_image_artifact_id = artifact.id,
        updated_at = clock_timestamp()
    FROM artifact
    WHERE active_recipe.id = artifact.recipe_id
      AND active_recipe.active_image_artifact_id IS DISTINCT FROM artifact.id
    RETURNING active_recipe.id
)
SELECT artifact.id::text,
       artifact.ref,
       recipe.ref,
       artifact.recipe_generation,
       artifact.promoted_reference,
       artifact.manifest_digest
FROM artifact
JOIN recipe ON recipe.id = artifact.recipe_id
LEFT JOIN activated ON activated.id = recipe.id
