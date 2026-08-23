-- name: commands_changeagentbinding_append_capability :exec
UPDATE control_plane.agents
SET capabilities=array_append(capabilities, $3)
WHERE organization_id=$1::uuid
  AND ref=$2
  AND NOT ($3=ANY(capabilities))
