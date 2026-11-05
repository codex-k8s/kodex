-- name: standard_oidc__session_revoke :exec
UPDATE control_plane.oidc_identity_sessions
SET state = 'REVOKED', revoked_at = COALESCE(revoked_at, clock_timestamp())
WHERE revision = @revision AND organization_id = @organization_id::uuid
  AND subject_id = @subject_id::uuid;
