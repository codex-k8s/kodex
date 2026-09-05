-- name: interaction_identity_revoke :one
UPDATE control_plane.interaction_identities
SET state='REVOKED',version=version+1,revoked_by=$4::uuid,revoked_at=clock_timestamp()
WHERE organization_id=$1::uuid AND ref=$2 AND version=$3 AND state='ACTIVE'
RETURNING ref;
