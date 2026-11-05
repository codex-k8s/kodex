-- name: standard_oidc__policy_insert :exec
INSERT INTO control_plane.oidc_identity_policies
    (revision, organization_id, issuer, digest, document)
VALUES (@revision, @organization_id::uuid, @issuer, @digest, @document::jsonb)
ON CONFLICT (revision) DO NOTHING;
