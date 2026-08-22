-- name: platform__repository_bootstrap_insert_owner_claim_contracts_organization_id_stable_key_state :exec
INSERT INTO control_plane.owner_claim_contracts (organization_id, stable_key, state)
		VALUES ($1::uuid, 'installation-owner', 'PENDING_CLAIM')
