-- name: standard_oidc__session_advance :exec
UPDATE control_plane.oidc_identity_sessions
SET last_issued_at = GREATEST(last_issued_at, @issued_at::timestamptz)
WHERE revision = @revision AND organization_id = @organization_id::uuid
  AND subject_id = @subject_id::uuid AND state = 'ACTIVE';
