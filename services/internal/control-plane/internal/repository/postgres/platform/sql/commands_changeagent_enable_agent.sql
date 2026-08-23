-- name: commands_changeagent_enable_agent :one
UPDATE control_plane.agents a SET enabled=$4,state=$5,version=version+1,updated_at=clock_timestamp() WHERE a.organization_id=$1::uuid AND a.ref=$2 AND a.version=$3 AND a.system_key IS NULL AND a.state<>'ARCHIVED' RETURNING a.project_id::text,a.ref,a.name,a.purpose,a.role_description,a.avatar_url,a.state,a.enabled,a.version,a.created_at,a.updated_at
