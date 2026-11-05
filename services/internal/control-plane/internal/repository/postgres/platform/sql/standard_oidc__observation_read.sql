-- name: standard_oidc__observation_read :one
SELECT last_issued_at, groups_digest, session_revision
FROM control_plane.oidc_identity_observations
WHERE organization_id = @organization_id::uuid AND subject_id = @subject_id::uuid;
