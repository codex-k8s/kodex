-- name: platform__runtime_delegateexecution_2 :one
SELECT a.id::text,a.name,a.role_description FROM control_plane.agents a WHERE a.organization_id=$1::uuid AND a.project_id=$2::uuid AND a.ref=$3 AND a.enabled AND a.state='READY'
