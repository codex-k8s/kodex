-- name: standard_oidc__policy_select_revision :one
SELECT organization_id::text, issuer, digest
FROM control_plane.oidc_identity_policies WHERE revision = @revision;
