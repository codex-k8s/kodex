-- name: platform__role_images_lock_artifact :one
SELECT artifact.id::text, artifact.ref, recipe.ref, artifact.spec_sha256, build.ref,
       artifact.staging_reference, artifact.manifest_digest,
       artifact.immutable_build_sha256, artifact.provenance_sha256,
       artifact.specification, artifact.policy_sha256, artifact.sbom_sha256,
       artifact.vulnerability_evidence_sha256, artifact.admission_verdict,
       artifact.signature_identity, artifact.signature_sha256,
       artifact.admission_receipt_sha256, artifact.admission_receipt_oci_manifest_digest,
       artifact.promoted_reference, artifact.promotion_readback_sha256,
       artifact.role_runtime_contract_sha256, artifact.version, artifact.recipe_version,
       artifact.recipe_generation, artifact.build_version, artifact.policy_revision,
       artifact.admission_revision, artifact.role_runtime_contract_revision,
       artifact.build_attempt, artifact.promoted_at, artifact.created_at, artifact.updated_at,
       artifact.admission_state, COALESCE(artifact.admission_claim_token_sha256, ''),
       artifact.admission_fence, artifact.admission_authority_generation,
       artifact.admission_claim_expires_at, artifact.promotion_state,
       COALESCE(artifact.promotion_claim_token_sha256, ''), artifact.promotion_fence,
       artifact.promotion_authority_generation, artifact.promotion_claim_expires_at,
       COALESCE(artifact.promotion_authorization_token_sha256, ''),
       artifact.promotion_authorization_expires_at, artifact.recipe_id::text
FROM control_plane.image_artifacts artifact
JOIN control_plane.role_image_recipes recipe ON recipe.id = artifact.recipe_id
JOIN control_plane.image_builds build ON build.id = artifact.build_id
WHERE artifact.organization_id = $1::uuid
  AND artifact.ref = $2
FOR UPDATE OF artifact
