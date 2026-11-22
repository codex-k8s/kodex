-- name: kubernetes_file_owner_source_version :one
SELECT version FROM control_plane.kubernetes_file_input_sources WHERE ref=$1;
