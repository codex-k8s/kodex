-- name: integration_approval__self_revoke :exec
UPDATE control_plane.integration_approval_scopes SET revoked_at=clock_timestamp()
WHERE organization_id=$1::uuid AND 'ias_'||replace(id::text,'-','')=$2 AND version=$3 AND revoked_at IS NULL
