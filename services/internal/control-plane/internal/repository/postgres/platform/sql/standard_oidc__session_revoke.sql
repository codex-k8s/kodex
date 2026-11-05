-- name: standard_oidc__session_revoke :one
WITH revoked AS (
    UPDATE control_plane.oidc_identity_sessions
    SET state = 'REVOKED', revoked_at = clock_timestamp()
    WHERE revision = @revision AND organization_id = @organization_id::uuid
      AND subject_id = @subject_id::uuid AND state = 'ACTIVE'
    RETURNING revision, organization_id, subject_id
), audit AS (
    INSERT INTO control_plane.audit_events
        (ref, organization_id, actor_id, action, resource_kind, resource_ref,
         outcome, safe_summary, correlation_ref)
    SELECT 'oidc_session_revoke_' || revision::text, organization_id, subject_id,
           'identity.session.revoke', 'OIDC_SESSION', revision::text,
           'SUCCEEDED', 'Own platform identity session revoked',
           'oidc_session_revoke_' || revision::text
    FROM revoked
    RETURNING id
)
SELECT EXISTS (SELECT 1 FROM revoked)
    OR EXISTS (
        SELECT 1 FROM control_plane.oidc_identity_sessions
        WHERE revision = @revision AND organization_id = @organization_id::uuid
          AND subject_id = @subject_id::uuid AND state = 'REVOKED'
    );
