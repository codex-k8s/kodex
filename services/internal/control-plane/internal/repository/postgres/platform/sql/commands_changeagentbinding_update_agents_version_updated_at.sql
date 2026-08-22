-- name: commands_changeagentbinding_update_agents_version_updated_at :exec
UPDATE control_plane.agents SET version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1::uuid AND ref=$2
