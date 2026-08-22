-- name: platform__commands_changeagentbinding_remove_agent_binding :exec
UPDATE control_plane.agents SET %s=array_remove(%s,$3) WHERE organization_id=$1::uuid AND ref=$2
