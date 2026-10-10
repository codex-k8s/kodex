-- #1258: состояние перехода policy без credential, specification и пользовательского текста.
BEGIN TRANSACTION READ ONLY;
SET LOCAL statement_timeout = '10s';
-- Исключается только необратимо superseded snapshot без owner request/effect.
-- Drift одного pin, неизвестный owner и прежний external claim остаются blocker.
WITH promotion_candidates AS (
    SELECT artifact.id, COALESCE(
        artifact.admission_state = 'ACCEPTED' AND artifact.admission_verdict = 'ACCEPTED'
        AND artifact.promotion_state = 'PENDING' AND artifact.promotion_request_id IS NULL
        AND artifact.promoted_reference = '' AND artifact.promotion_fence = 0
        AND artifact.promotion_authority_generation = 0 AND artifact.promotion_claimant_workload IS NULL
        AND artifact.promotion_claim_token_sha256 IS NULL AND artifact.promotion_claim_expires_at IS NULL
        AND artifact.promotion_authorization_token_sha256 IS NULL AND artifact.promotion_authorization_expires_at IS NULL
        AND NOT EXISTS (SELECT 1 FROM control_plane.role_image_promotion_requests request
            WHERE request.image_artifact_id = artifact.id)
        AND artifact.spec_sha256 ~ '^[a-f0-9]{64}$' AND artifact.policy_sha256 ~ '^[a-f0-9]{64}$'
        AND artifact.policy_revision > 0 AND artifact.role_runtime_contract_revision > 0
        AND artifact.role_runtime_contract_sha256 ~ '^[a-f0-9]{64}$'
        AND artifact.manifest_digest ~ '^sha256:[a-f0-9]{64}$'
        AND artifact.provenance_sha256 ~ '^[a-f0-9]{64}$' AND artifact.immutable_build_sha256 ~ '^[a-f0-9]{64}$'
        AND artifact.sbom_sha256 ~ '^[a-f0-9]{64}$' AND artifact.vulnerability_evidence_sha256 ~ '^[a-f0-9]{64}$'
        AND artifact.signature_identity <> '' AND artifact.signature_sha256 ~ '^[a-f0-9]{64}$'
        AND artifact.admission_revision > 0 AND artifact.admission_receipt_sha256 ~ '^[a-f0-9]{64}$'
        AND artifact.admission_receipt_oci_manifest_digest ~ '^sha256:[a-f0-9]{64}$'
        AND artifact.version > 0 AND artifact.recipe_version > 0 AND artifact.recipe_generation > 0
        AND EXISTS (
            SELECT 1 FROM control_plane.role_image_recipes recipe
            JOIN control_plane.organizations organization ON organization.id = recipe.organization_id
            JOIN control_plane.image_builds build ON build.id = artifact.build_id
            WHERE recipe.id = artifact.recipe_id AND recipe.organization_id = artifact.organization_id
              AND recipe.project_id IS NOT DISTINCT FROM artifact.project_id
              AND recipe.scope_kind = artifact.scope_kind AND build.scope_kind = artifact.scope_kind
              AND ((artifact.scope_kind = 'ORGANIZATION' AND artifact.project_id IS NULL)
                  OR (artifact.scope_kind = 'PROJECT' AND artifact.project_id IS NOT NULL AND EXISTS (
                      SELECT 1 FROM control_plane.projects project
                      WHERE project.id = artifact.project_id AND project.organization_id = artifact.organization_id)))
              AND recipe.state = 'ACTIVE'
              AND recipe.version > artifact.recipe_version AND recipe.generation > artifact.recipe_generation
              AND recipe.spec_sha256 ~ '^[a-f0-9]{64}$' AND recipe.policy_sha256 ~ '^[a-f0-9]{64}$'
              AND recipe.policy_revision > 0 AND recipe.role_runtime_contract_revision > 0
              AND recipe.role_runtime_contract_sha256 ~ '^[a-f0-9]{64}$'
              AND build.organization_id = artifact.organization_id AND build.recipe_id = recipe.id
              AND build.project_id IS NOT DISTINCT FROM artifact.project_id
              AND build.stage = 'COMPLETED' AND build.version = artifact.build_version
              AND build.recipe_version = artifact.recipe_version AND build.recipe_generation = artifact.recipe_generation
              AND build.spec_sha256 = artifact.spec_sha256 AND build.attempt = artifact.build_attempt
              AND artifact.build_version > 0 AND artifact.build_attempt > 0
        ), false) AS superseded_unrequested
    FROM control_plane.image_artifacts artifact
    -- Начальный PENDING не означает promotion: admission PENDING/CLAIMED считается отдельно,
    -- REJECTED/FAILED без запроса/claim не имеет допуска к внешнему эффекту.
    WHERE (artifact.admission_state = 'ACCEPTED' AND artifact.promotion_state = 'PENDING')
       OR artifact.promotion_state IN ('CLAIMED', 'AUTHORIZED')
       OR artifact.admission_state IS NULL OR artifact.admission_state NOT IN ('PENDING', 'CLAIMED', 'ACCEPTED', 'REJECTED', 'FAILED')
       OR artifact.promotion_state IS NULL OR artifact.promotion_state NOT IN ('PENDING', 'CLAIMED', 'AUTHORIZED', 'PROMOTED', 'REJECTED')
       OR artifact.promotion_claimant_workload IS NOT NULL OR artifact.promotion_authority_generation > 0
       OR artifact.promotion_claim_token_sha256 IS NOT NULL OR artifact.promotion_claim_expires_at IS NOT NULL
       OR artifact.promotion_authorization_token_sha256 IS NOT NULL OR artifact.promotion_authorization_expires_at IS NOT NULL
       OR (artifact.promotion_state = 'PENDING' AND artifact.promotion_request_id IS NOT NULL)
       OR EXISTS (SELECT 1 FROM control_plane.role_image_promotion_requests request
           WHERE request.image_artifact_id = artifact.id
             AND (request.state IS NULL OR request.state NOT IN ('PROMOTED', 'FAILED')))
)
SELECT jsonb_build_object(
    'at', clock_timestamp(),
    'openBuilds', (SELECT count(*) FROM control_plane.image_builds
        WHERE stage NOT IN ('COMPLETED', 'FAILED', 'CANCELLED', 'EXPIRED', 'DEAD_LETTER')),
    'pendingAdmissions', (SELECT count(*) FROM control_plane.image_artifacts
        WHERE admission_state IN ('PENDING', 'CLAIMED')),
    'pendingPromotions', (SELECT count(*) FROM promotion_candidates WHERE NOT superseded_unrequested),
    'supersededUnrequestedPromotions', (SELECT count(*) FROM promotion_candidates WHERE superseded_unrequested),
    'activeRuntimeRuns', (SELECT count(*) FROM control_plane.runs
        WHERE state IN ('QUEUED', 'RUNNING', 'CANCELLING')),
    'claimedRuntimeLeases', (SELECT count(*) FROM control_plane.runtime_leases WHERE state = 'CLAIMED'),
    'promotedArtifactCount', (SELECT count(*) FROM control_plane.image_artifacts WHERE promotion_state = 'PROMOTED'),
    'promotedPinsSHA256', (SELECT encode(sha256(convert_to(COALESCE(string_agg(
        concat_ws(':', id::text, manifest_digest, promoted_reference, policy_sha256, version::text),
        E'\n' ORDER BY id), ''), 'UTF8')), 'hex')
        FROM control_plane.image_artifacts WHERE promotion_state = 'PROMOTED')
)::text;
COMMIT;
