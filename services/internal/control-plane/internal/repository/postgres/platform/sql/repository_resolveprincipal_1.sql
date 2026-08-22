-- name: platform__repository_resolveprincipal_1 :one
SELECT o.id::text,o.ref,COALESCE(o.authority_tenant_ref,''),c.state
		FROM control_plane.organizations o JOIN control_plane.owner_claim_contracts c ON c.organization_id=o.id
		LIMIT 1 FOR UPDATE OF o,c
