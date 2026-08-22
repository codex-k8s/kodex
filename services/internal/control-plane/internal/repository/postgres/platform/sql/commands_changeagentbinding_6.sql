-- name: platform__commands_changeagentbinding_6 :exec
UPDATE control_plane.agents SET version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1::uuid AND ref=$2
