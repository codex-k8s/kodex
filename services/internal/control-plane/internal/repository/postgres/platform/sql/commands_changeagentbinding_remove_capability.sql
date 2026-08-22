-- name: platform__commands_changeagentbinding_remove_capability :exec
UPDATE control_plane.agents
SET capabilities=array_remove(capabilities, $3)
WHERE organization_id=$1::uuid
  AND ref=$2
