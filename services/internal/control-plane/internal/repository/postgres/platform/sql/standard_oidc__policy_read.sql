-- name: standard_oidc__policy_read :one
SELECT p.organization_id::text, p.revision, p.issuer, p.digest, p.document
FROM control_plane.oidc_identity_policy_current c
JOIN control_plane.oidc_identity_policies p ON p.revision = c.revision
WHERE c.singleton;
