-- name: platform__repository_resolveprincipal_5 :exec
UPDATE control_plane.owner_claim_contracts
			SET state='CLAIMED',subject_id=$2::uuid,claimed_at=clock_timestamp(),version=version+1 WHERE organization_id=$1::uuid
