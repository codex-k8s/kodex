-- name: standard_oidc__session_subject_read :one
SELECT session.revision, session.organization_id::text, session.subject_id::text,
       session.issuer, session.external_subject::text, session.provider_session_id::text,
       session.authenticated_at, session.absolute_expires_at, session.last_issued_at,
       session.policy_revision, session.policy_digest, session.state, subject.active
FROM control_plane.oidc_identity_sessions session
JOIN control_plane.subjects subject
  ON subject.organization_id = session.organization_id AND subject.id = session.subject_id
WHERE session.revision = @revision AND session.issuer = @issuer
  AND session.external_subject = @external_subject::uuid;
