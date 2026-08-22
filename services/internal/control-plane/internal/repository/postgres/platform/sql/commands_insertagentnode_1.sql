-- name: platform__commands_insertagentnode_1 :one
SELECT id::text,role_description FROM control_plane.agents WHERE organization_id=$1::uuid AND ref=$2 AND enabled AND state='READY'
