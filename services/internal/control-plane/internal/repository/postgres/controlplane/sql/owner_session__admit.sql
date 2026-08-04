INSERT INTO control_plane.owner_oidc_sessions (
    organization_id, actor_id, session_id, credential_digest_sha256,
    current_revision, revoked_at, updated_at
) VALUES (
    @organization_id, @actor_id, @session_id, @credential_digest_sha256,
    @current_revision, NULL, @updated_at
)
ON CONFLICT (organization_id, actor_id, session_id) DO UPDATE
SET credential_digest_sha256 = EXCLUDED.credential_digest_sha256,
    current_revision = EXCLUDED.current_revision,
    revoked_at = NULL,
    updated_at = EXCLUDED.updated_at
WHERE (
        EXCLUDED.current_revision = owner_oidc_sessions.current_revision
        AND EXCLUDED.credential_digest_sha256 = owner_oidc_sessions.credential_digest_sha256
        AND owner_oidc_sessions.revoked_at IS NULL
    ) OR EXCLUDED.current_revision = owner_oidc_sessions.current_revision + 1
RETURNING organization_id, actor_id, session_id, credential_digest_sha256,
    current_revision, revoked_at, updated_at;
