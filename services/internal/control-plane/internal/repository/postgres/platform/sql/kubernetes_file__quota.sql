-- name: kubernetes_file__quota :one
SELECT coalesce(sum(coalesce(byte_count,maximum_bytes)),0),count(*)
FROM control_plane.kubernetes_file_input_sources WHERE organization_id=@organization_id::uuid AND payload_purged_at IS NULL;
