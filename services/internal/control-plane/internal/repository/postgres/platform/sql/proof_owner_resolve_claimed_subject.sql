-- name: proof_owner_resolve_claimed_subject :one
SELECT o.id::text, o.version, s.id::text
FROM control_plane.organizations o
JOIN control_plane.owner_claim_contracts c
  ON c.organization_id = o.id
JOIN control_plane.subjects s
  ON s.organization_id = o.id
WHERE c.state = 'CLAIMED'
  AND o.authority_tenant_ref = $1
  AND s.external_subject_digest = $2
  AND s.active
LIMIT 1
