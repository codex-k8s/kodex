-- name: platform__commands_changeagentbinding_4 :exec
UPDATE control_plane.agents SET %s=array_append(%s,$3) WHERE organization_id=$1::uuid AND ref=$2 AND NOT ($3=ANY(%s))
