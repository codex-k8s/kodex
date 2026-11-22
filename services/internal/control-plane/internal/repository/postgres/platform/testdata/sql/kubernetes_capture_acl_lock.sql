-- name: kubernetes_capture_acl_lock :one
SELECT ref FROM control_plane.kubernetes_output_captures WHERE ref=$1 AND organization_id=$2::uuid FOR UPDATE;
