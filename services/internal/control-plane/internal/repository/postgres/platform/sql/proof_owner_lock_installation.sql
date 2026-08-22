-- name: platform__proof_owner_lock_installation :one
SELECT o.id::text, o.version, COALESCE(o.authority_tenant_ref, ''), c.state
FROM control_plane.organizations o
JOIN control_plane.owner_claim_contracts c ON c.organization_id = o.id
LIMIT 1
FOR UPDATE OF o, c
