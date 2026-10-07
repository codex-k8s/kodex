-- name: supply_chain_owner_readback :one
-- #1797: точная работа владельца для maintenance, без credential/specification.
-- ACCEPTED/PENDING без owner Publish не является поставленной promotion task.
BEGIN TRANSACTION READ ONLY;
SET LOCAL statement_timeout = '10s';
SELECT jsonb_build_object(
    'at', clock_timestamp(),
    'openBuilds', (SELECT count(*) FROM control_plane.image_builds
        WHERE stage NOT IN ('COMPLETED', 'FAILED', 'CANCELLED', 'EXPIRED', 'DEAD_LETTER')),
    'pendingAdmissions', (SELECT count(*) FROM control_plane.image_artifacts
        WHERE admission_state IN ('PENDING', 'CLAIMED')),
    'pendingPromotions',
        (SELECT count(*) FROM control_plane.role_image_promotion_requests
            WHERE state IN ('QUEUED', 'PROMOTING')) +
        (SELECT count(*) FROM control_plane.image_artifacts artifact
            WHERE artifact.promotion_state IN ('CLAIMED', 'AUTHORIZED')
            AND NOT EXISTS (SELECT 1 FROM control_plane.role_image_promotion_requests request
                WHERE request.id = artifact.promotion_request_id
                    AND request.image_artifact_id = artifact.id
                    AND request.organization_id = artifact.organization_id
                    AND request.project_id IS NOT DISTINCT FROM artifact.project_id
                    AND request.state IN ('QUEUED', 'PROMOTING'))),
    'unrequestedAcceptedArtifacts', (SELECT count(*) FROM control_plane.image_artifacts
        WHERE admission_state = 'ACCEPTED' AND promotion_state = 'PENDING'
            AND promotion_request_id IS NULL),
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
