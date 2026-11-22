-- name: ssh_health__connection_id :one
SELECT id::text FROM control_plane.integration_connections WHERE organization_id=$1::uuid AND ref=$2;
