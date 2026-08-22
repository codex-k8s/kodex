-- name: platform__commands_changeagentbinding_3 :exec
UPDATE control_plane.integration_grants SET enabled=false,version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1::uuid AND ref=$2 AND target_kind='AGENT' AND target_ref=$3
