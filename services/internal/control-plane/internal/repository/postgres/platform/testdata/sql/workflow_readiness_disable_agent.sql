-- name: workflow_readiness_disable_agent :exec
UPDATE control_plane.agents SET enabled=false
WHERE organization_id=$1::uuid AND ref=$2;
