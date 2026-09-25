-- name: role_images_get_promotion_candidate :one
SELECT artifact.ref, recipe.ref, artifact.spec_sha256, build.ref, artifact.staging_reference,
       artifact.manifest_digest, artifact.immutable_build_sha256, artifact.provenance_sha256,
       artifact.specification, artifact.policy_sha256, artifact.sbom_sha256,
       artifact.vulnerability_evidence_sha256,
       CASE WHEN artifact.admission_state = 'REJECTED' THEN 'REJECTED' ELSE artifact.admission_verdict END,
       artifact.signature_identity, artifact.signature_sha256,
       artifact.admission_receipt_sha256, artifact.admission_receipt_oci_manifest_digest,
       artifact.promoted_reference, artifact.promotion_readback_sha256,
       artifact.role_runtime_contract_sha256, artifact.version, artifact.recipe_version,
       artifact.recipe_generation, artifact.build_version, artifact.policy_revision,
       artifact.admission_revision, artifact.role_runtime_contract_revision,
       artifact.build_attempt, artifact.promoted_at, artifact.created_at, artifact.updated_at,
       artifact.admission_state = 'ACCEPTED'
           AND artifact.admission_verdict = 'ACCEPTED'
           AND artifact.promotion_request_id IS NULL
           AND artifact.promotion_state = 'PENDING' AS promotable
FROM control_plane.image_artifacts artifact
JOIN control_plane.role_image_recipes recipe ON recipe.id = artifact.recipe_id
JOIN control_plane.image_builds build ON build.id = artifact.build_id
WHERE artifact.organization_id = $1::uuid
  AND artifact.recipe_id = $2::uuid
  AND recipe.organization_id = artifact.organization_id
  AND recipe.state = 'ACTIVE'
  AND artifact.recipe_version = recipe.version
  AND artifact.recipe_generation = recipe.generation
  AND artifact.spec_sha256 = recipe.spec_sha256
  AND artifact.policy_revision = recipe.policy_revision
  AND artifact.policy_sha256 = recipe.policy_sha256
  AND artifact.role_runtime_contract_revision = recipe.role_runtime_contract_revision
  AND artifact.role_runtime_contract_sha256 = recipe.role_runtime_contract_sha256
  AND build.id = (
      SELECT latest.id
      FROM control_plane.image_builds latest
      WHERE latest.organization_id = artifact.organization_id
        AND latest.recipe_id = artifact.recipe_id
      ORDER BY latest.created_at DESC, latest.updated_at DESC, latest.attempt DESC, latest.ref ASC
      LIMIT 1
  )
  AND artifact.promoted_reference = ''
  AND (
      (
          artifact.admission_state = 'ACCEPTED'
          AND artifact.admission_verdict = 'ACCEPTED'
          AND artifact.promotion_state IN ('PENDING', 'CLAIMED', 'AUTHORIZED')
          AND artifact.manifest_digest ~ '^sha256:[a-f0-9]{64}$'
          AND artifact.provenance_sha256 ~ '^[a-f0-9]{64}$'
          AND artifact.immutable_build_sha256 ~ '^[a-f0-9]{64}$'
          AND artifact.sbom_sha256 ~ '^[a-f0-9]{64}$'
          AND artifact.vulnerability_evidence_sha256 ~ '^[a-f0-9]{64}$'
          AND artifact.signature_identity <> ''
          AND artifact.signature_sha256 ~ '^[a-f0-9]{64}$'
          AND artifact.admission_revision > 0
          AND artifact.admission_receipt_sha256 ~ '^[a-f0-9]{64}$'
          AND artifact.admission_receipt_oci_manifest_digest ~ '^sha256:[a-f0-9]{64}$'
      )
      OR (
          artifact.admission_state = 'REJECTED'
          AND artifact.promotion_state = 'REJECTED'
      )
  )
ORDER BY artifact.created_at DESC, artifact.ref DESC
LIMIT 1
