-- name: platform__permissions_projectidbyresource_9 :many
SELECT project_id::text FROM control_plane.assistant_conversations WHERE organization_id=$1::uuid AND ref=$2 AND project_id IS NOT NULL
