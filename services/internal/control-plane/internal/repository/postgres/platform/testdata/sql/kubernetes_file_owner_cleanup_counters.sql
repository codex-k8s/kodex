-- name: kubernetes_file_owner_cleanup_counters :one
SELECT version,cleanup_attempts,cleanup_total_attempts FROM control_plane.kubernetes_file_input_sources WHERE ref=$1 AND payload_purged_at IS NOT NULL;
