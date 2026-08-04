SELECT EXISTS (
    SELECT 1
    FROM control_plane.owner_oidc_sessions
    WHERE organization_id = @organization_id
      AND actor_id = @actor_id
      AND session_id = @session_id
      AND credential_digest_sha256 = @credential_digest_sha256
      AND current_revision = @current_revision
      AND (revoked_at IS NULL OR @allow_revoked)
);
