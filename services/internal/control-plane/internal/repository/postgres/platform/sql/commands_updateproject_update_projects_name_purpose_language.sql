-- name: commands_updateproject_update_projects_name_purpose_language :one
UPDATE control_plane.projects SET name=$4,purpose=$5,language=$6,version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1::uuid AND ref=$2 AND version=$3 RETURNING id::text,ref,name,purpose,language,lifecycle,version,created_at,updated_at
