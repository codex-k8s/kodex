-- name: standard_oidc__observation_advance :exec
INSERT INTO control_plane.oidc_identity_observations
    (organization_id, subject_id, last_issued_at, groups_digest, session_revision, observed_at)
VALUES (@organization_id::uuid, @subject_id::uuid, @issued_at, @groups_digest, @revision, clock_timestamp())
ON CONFLICT (organization_id, subject_id) DO UPDATE
SET last_issued_at = EXCLUDED.last_issued_at, groups_digest = EXCLUDED.groups_digest,
    session_revision = GREATEST(oidc_identity_observations.session_revision, EXCLUDED.session_revision),
    observed_at = clock_timestamp()
WHERE EXCLUDED.last_issued_at >= oidc_identity_observations.last_issued_at
  AND (EXCLUDED.last_issued_at > oidc_identity_observations.last_issued_at
       OR EXCLUDED.groups_digest = oidc_identity_observations.groups_digest);
