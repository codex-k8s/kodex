-- name: proof_owner_claim_installation :exec
WITH claim AS (
    UPDATE control_plane.owner_claim_contracts
    SET state = 'CLAIMED', subject_id = $2::uuid, claimed_at = clock_timestamp(), version = version + 1
    WHERE organization_id = $1::uuid AND state = 'PENDING_CLAIM'
    RETURNING organization_id
)
UPDATE control_plane.organizations
SET authority_tenant_ref = $3, version = version + 1, updated_at = clock_timestamp()
WHERE id IN (SELECT organization_id FROM claim)
