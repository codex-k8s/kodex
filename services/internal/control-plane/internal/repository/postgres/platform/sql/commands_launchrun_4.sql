-- name: platform__commands_launchrun_4 :one
SELECT id::text FROM control_plane.sessions WHERE organization_id=$1::uuid AND project_id=$2::uuid AND ref=$3 AND target_type=$4 AND target_ref=$5 AND state='ACTIVE' FOR UPDATE
