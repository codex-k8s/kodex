-- #1258: состояние перехода policy без credential, specification и пользовательского текста.
BEGIN TRANSACTION READ ONLY;
SET LOCAL statement_timeout = '10s';
SELECT jsonb_build_object(
    'at', clock_timestamp(),
    'openBuilds', (SELECT count(*) FROM control_plane.image_builds
        WHERE stage NOT IN ('COMPLETED', 'FAILED', 'CANCELLED', 'EXPIRED', 'DEAD_LETTER')),
    'pendingAdmissions', (SELECT count(*) FROM control_plane.image_artifacts
        WHERE admission_state IN ('PENDING', 'CLAIMED')),
    'pendingPromotions', (SELECT count(*) FROM control_plane.image_artifacts
        WHERE admission_state = 'ACCEPTED' AND promotion_state IN ('PENDING', 'CLAIMED', 'AUTHORIZED')),
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
