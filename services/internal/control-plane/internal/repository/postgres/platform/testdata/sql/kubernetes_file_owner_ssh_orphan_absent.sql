-- name: kubernetes_file_owner_ssh_orphan_absent :one
SELECT count(*) FROM control_plane.ssh_execution_observations WHERE intent_id=$1::uuid AND organization_id=$2::uuid;
