UPDATE control_plane.owner_oidc_sessions
SET revoked_at = COALESCE(revoked_at, @updated_at),
    updated_at = GREATEST(updated_at, @updated_at)
WHERE organization_id = @organization_id
  AND actor_id = @actor_id
  AND session_id = @session_id
  AND credential_digest_sha256 = @credential_digest_sha256
  AND current_revision = @current_revision
RETURNING organization_id, actor_id, session_id, credential_digest_sha256,
    current_revision, revoked_at, updated_at;
