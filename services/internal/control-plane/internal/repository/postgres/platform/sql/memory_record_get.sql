-- name: memory_record_get :one
SELECT id::text,project_id::text,COALESCE(agent_id::text,''),COALESCE(current_revision_id::text,''),projection
FROM control_plane.memory_record_projection WHERE organization_id=$1::uuid AND ref=$2;
