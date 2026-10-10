-- #1797: bounded owner diagnostic; не command, не actor authority и не idle proof.
-- Поля specification, source content, credentials и claim tokens не проецируются.
BEGIN TRANSACTION READ ONLY;
SET LOCAL statement_timeout = '10s';
WITH pending AS MATERIALIZED (
    SELECT artifact.id, artifact.ref
    FROM control_plane.image_artifacts artifact
    WHERE artifact.admission_state = 'ACCEPTED'
      AND artifact.promotion_state IN ('PENDING', 'CLAIMED', 'AUTHORIZED')
), page AS (
    SELECT id, ref FROM pending ORDER BY ref LIMIT 64
), facts AS (
    SELECT artifact.ref AS artifact_ref, recipe.ref AS recipe_ref,
           build.ref AS build_ref, organization.ref AS organization_ref,
           COALESCE(project.ref, '') AS project_ref, request.ref AS request_ref,
           artifact.scope_kind, artifact.version AS artifact_version,
           recipe.version AS recipe_version, artifact.recipe_version AS artifact_recipe_version,
           recipe.generation AS recipe_generation, artifact.recipe_generation AS artifact_recipe_generation,
           build.version AS build_version, artifact.build_version AS artifact_build_version,
           artifact.admission_state, artifact.promotion_state, recipe.state AS recipe_state,
           build.stage AS build_state, request.state AS request_state,
           artifact.promotion_request_id IS NOT NULL AS promotion_requested,
           artifact.promoted_reference <> '' AS promoted_reference_present,
           COALESCE(artifact.promotion_claim_expires_at <= statement_timestamp(), false) AS claim_expired,
           COALESCE(artifact.promotion_authorization_expires_at <= statement_timestamp(), false) AS authorization_expired,
           COALESCE(recipe.organization_id = artifact.organization_id
               AND build.organization_id = artifact.organization_id
               AND build.recipe_id = recipe.id AND artifact.recipe_id = recipe.id
               AND recipe.project_id IS NOT DISTINCT FROM artifact.project_id
               AND recipe.scope_kind = artifact.scope_kind, false) AS owner_scope_consistent,
           COALESCE(recipe.version = artifact.recipe_version
               AND recipe.generation = artifact.recipe_generation
               AND recipe.spec_sha256 = artifact.spec_sha256
               AND recipe.policy_revision = artifact.policy_revision
               AND recipe.policy_sha256 = artifact.policy_sha256
               AND recipe.role_runtime_contract_revision = artifact.role_runtime_contract_revision
               AND recipe.role_runtime_contract_sha256 = artifact.role_runtime_contract_sha256, false) AS recipe_pins_match,
           COALESCE(latest.id = artifact.build_id, false) AS latest_native_build,
           artifact.admission_verdict = 'ACCEPTED'
               AND artifact.manifest_digest ~ '^sha256:[a-f0-9]{64}$'
               AND artifact.provenance_sha256 ~ '^[a-f0-9]{64}$'
               AND artifact.immutable_build_sha256 ~ '^[a-f0-9]{64}$'
               AND artifact.sbom_sha256 ~ '^[a-f0-9]{64}$'
               AND artifact.vulnerability_evidence_sha256 ~ '^[a-f0-9]{64}$'
               AND artifact.signature_identity <> ''
               AND artifact.signature_sha256 ~ '^[a-f0-9]{64}$'
               AND artifact.admission_revision > 0
               AND artifact.admission_receipt_sha256 ~ '^[a-f0-9]{64}$'
               AND artifact.admission_receipt_oci_manifest_digest ~ '^sha256:[a-f0-9]{64}$' AS native_evidence_complete,
           COALESCE(request.organization_id = artifact.organization_id
               AND request.image_artifact_id = artifact.id AND request.recipe_id = recipe.id
               AND request.project_id IS NOT DISTINCT FROM artifact.project_id
               AND request.expected_provenance_sha256 = artifact.provenance_sha256
               AND request.manifest_digest = artifact.manifest_digest
               AND request.receipt_sha256 ~ '^[a-f0-9]{64}$', false) AS request_tuple_matches
    FROM page
    JOIN control_plane.image_artifacts artifact ON artifact.id = page.id
    LEFT JOIN control_plane.role_image_recipes recipe ON recipe.id = artifact.recipe_id
    LEFT JOIN control_plane.image_builds build ON build.id = artifact.build_id
    LEFT JOIN control_plane.organizations organization ON organization.id = artifact.organization_id
    LEFT JOIN control_plane.projects project ON project.id = artifact.project_id
    LEFT JOIN control_plane.role_image_promotion_requests request ON request.id = artifact.promotion_request_id
    LEFT JOIN LATERAL (
        SELECT candidate.id FROM control_plane.image_builds candidate
        WHERE candidate.organization_id = artifact.organization_id AND candidate.recipe_id = artifact.recipe_id
        ORDER BY candidate.created_at DESC, candidate.updated_at DESC, candidate.attempt DESC, candidate.ref ASC
        LIMIT 1
    ) latest ON true
), items AS (
    SELECT artifact_ref, jsonb_build_object(
        'scopeKind', scope_kind, 'organizationRef', organization_ref, 'projectRef', project_ref,
        'recipeRef', recipe_ref, 'buildRef', build_ref, 'artifactRef', artifact_ref, 'promotionRequestRef', request_ref,
        'artifactVersion', artifact_version, 'recipeVersion', recipe_version, 'artifactRecipeVersion', artifact_recipe_version,
        'recipeGeneration', recipe_generation, 'artifactRecipeGeneration', artifact_recipe_generation,
        'buildVersion', build_version, 'artifactBuildVersion', artifact_build_version,
        'admissionState', admission_state, 'promotionState', promotion_state,
        'recipeState', recipe_state, 'buildState', build_state, 'requestState', request_state,
        'promotionRequested', promotion_requested, 'promotedReferencePresent', promoted_reference_present,
        'claimExpired', claim_expired, 'authorizationExpired', authorization_expired,
        'ownerScopeConsistent', owner_scope_consistent, 'recipePinsMatch', recipe_pins_match,
        'latestNativeBuild', latest_native_build, 'nativeEvidenceComplete', native_evidence_complete,
        'requestTupleMatches', request_tuple_matches,
        'nativeCandidateStructurallyVisible', owner_scope_consistent AND recipe_state = 'ACTIVE'
            AND recipe_pins_match AND latest_native_build AND NOT promoted_reference_present AND native_evidence_complete,
        'unrequestedCurrentCandidate', owner_scope_consistent AND recipe_state = 'ACTIVE'
            AND recipe_pins_match AND latest_native_build AND NOT promoted_reference_present
            AND native_evidence_complete AND promotion_state = 'PENDING' AND NOT promotion_requested,
        'workerClaimStructurallySelectable', owner_scope_consistent AND recipe_state = 'ACTIVE'
            AND recipe_pins_match AND NOT promoted_reference_present AND native_evidence_complete AND request_tuple_matches
            AND ((request_state = 'QUEUED' AND promotion_state = 'PENDING')
                OR (request_state = 'PROMOTING' AND promotion_state = 'CLAIMED' AND claim_expired)
                OR (request_state = 'PROMOTING' AND promotion_state = 'AUTHORIZED' AND authorization_expired))
    ) AS item FROM facts
), document AS (
    SELECT jsonb_build_object(
        'version', 1, 'kind', 'RUNNER_PENDING_PROMOTIONS_READBACK', 'status', 'OBSERVED',
        'at', statement_timestamp(), 'total', (SELECT count(*) FROM pending), 'limit', 64,
        'complete', (SELECT count(*) FROM pending) <= 64,
        'items', COALESCE((SELECT jsonb_agg(item ORDER BY artifact_ref) FROM items), '[]'::jsonb)
    ) AS body
)
-- Даже при ошибочной будущей schema рост body закрыто отменяет выдачу items.
SELECT CASE WHEN octet_length(body::text) <= 131072 THEN body
    ELSE jsonb_build_object('version', 1, 'kind', 'RUNNER_PENDING_PROMOTIONS_READBACK',
        'status', 'BODY_LIMIT_EXCEEDED', 'complete', false, 'items', '[]'::jsonb)
    END::text FROM document;
COMMIT;
