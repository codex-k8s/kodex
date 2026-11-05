-- name: standard_oidc__installation :one
SELECT o.id::text
FROM control_plane.organizations o
JOIN control_plane.owner_claim_contracts c ON c.organization_id = o.id
WHERE c.stable_key = 'installation-owner';
