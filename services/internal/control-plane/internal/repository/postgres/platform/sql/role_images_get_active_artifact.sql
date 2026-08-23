-- name: role_images_get_active_artifact :one
SELECT artifact.ref, recipe.ref, artifact.spec_sha256, build.ref, artifact.staging_reference,
       artifact.manifest_digest, artifact.immutable_build_sha256, artifact.provenance_sha256,
       artifact.specification, artifact.policy_sha256, artifact.sbom_sha256,
       artifact.vulnerability_evidence_sha256, artifact.admission_verdict,
       artifact.signature_identity, artifact.signature_sha256,
       artifact.admission_receipt_sha256, artifact.admission_receipt_oci_manifest_digest,
       artifact.promoted_reference, artifact.promotion_readback_sha256,
       artifact.role_runtime_contract_sha256, artifact.version, artifact.recipe_version,
       artifact.recipe_generation, artifact.build_version, artifact.policy_revision,
       artifact.admission_revision, artifact.role_runtime_contract_revision,
       artifact.build_attempt, artifact.promoted_at, artifact.created_at, artifact.updated_at
FROM control_plane.image_artifacts artifact
JOIN control_plane.role_image_recipes recipe ON recipe.id = artifact.recipe_id
JOIN control_plane.image_builds build ON build.id = artifact.build_id
WHERE artifact.organization_id = $1::uuid
  AND artifact.ref = $2
