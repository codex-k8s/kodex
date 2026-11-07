-- name: standard_oidc__session_insert :exec
INSERT INTO control_plane.oidc_identity_sessions
    (revision, organization_id, subject_id, issuer, external_subject, provider_session_id,
     authenticated_at, absolute_expires_at, last_issued_at, policy_revision, policy_digest)
VALUES (@revision, @organization_id::uuid, @subject_id::uuid, @issuer, @external_subject::uuid,
        @session_id::uuid, @authenticated_at, @absolute_expires_at, @issued_at, @policy_revision, @policy_digest);
