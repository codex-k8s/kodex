-- name: role_images_superseded_expiry_readback :one
SELECT artifact.admission_state, artifact.admission_verdict,
       artifact.version, artifact.admission_fence,
       artifact.admission_claimant_workload IS NOT NULL
          AND artifact.admission_authority_generation>0
          AND artifact.admission_claim_token_sha256 IS NOT NULL
          AND artifact.admission_claim_expires_at IS NOT NULL,
       artifact.admission_claimant_workload IS NULL AND artifact.admission_authority_generation=0
          AND artifact.admission_claim_token_sha256 IS NULL AND artifact.admission_claim_expires_at IS NULL,
       artifact.promotion_state='REJECTED' AND artifact.promotion_claimant_workload IS NULL
          AND artifact.promotion_authority_generation=0 AND artifact.promotion_claim_token_sha256 IS NULL
          AND artifact.promotion_claim_expires_at IS NULL AND artifact.promotion_authorization_token_sha256 IS NULL
          AND artifact.promotion_authorization_expires_at IS NULL,
       attempt.state, attempt.version, attempt.fence,
       attempt.finished_at IS NOT NULL AND attempt.terminal_artifact_json IS NOT NULL,
       COALESCE(attempt.terminal_artifact_json=to_jsonb(artifact),false),
       attempt.organization_id=artifact.organization_id
          AND attempt.source_artifact_json->>'Ref'=artifact.ref
          AND attempt.source_artifact_json->>'BuildRef'=(SELECT ref FROM control_plane.image_builds WHERE id=artifact.build_id)
          AND attempt.source_artifact_json->>'RecipeGeneration'=artifact.recipe_generation::text
          AND attempt.source_artifact_json->>'SpecSHA256'=artifact.spec_sha256
          AND attempt.source_artifact_json->>'ManifestDigest'=artifact.manifest_digest
          AND attempt.source_artifact_json->>'ImmutableBuildSHA256'=artifact.immutable_build_sha256
          AND attempt.source_artifact_json->>'ProvenanceSHA256'=artifact.provenance_sha256,
       (SELECT count(*) FROM control_plane.audit_events audit
        WHERE audit.organization_id=artifact.organization_id AND audit.resource_ref=artifact.ref
          AND audit.action='platform.role-images.admission.expire'),
       (SELECT count(*) FROM control_plane.idempotency_receipts receipt
        WHERE receipt.organization_id=artifact.organization_id AND receipt.operation='platform.role-images.admission.expire')
FROM control_plane.image_artifacts artifact
JOIN control_plane.image_admission_attempts attempt ON attempt.artifact_id=artifact.id
WHERE artifact.ref=$1 AND attempt.ref=$2;
