-- name: configuration_changeintegrationgrant_policy_change_active :one
SELECT EXISTS (
    SELECT 1 FROM control_plane.integration_grants grant_row
    JOIN control_plane.integration_invocations invocation ON invocation.grant_id=grant_row.id
    WHERE grant_row.organization_id=$1::uuid AND grant_row.connection_id=$2::uuid
      AND grant_row.capability_key=$3 AND grant_row.target_kind=$4 AND grant_row.target_ref=$5
      AND invocation.state IN ('WAITING_APPROVAL','READY','RUNNING','UNKNOWN_OUTCOME')
)
