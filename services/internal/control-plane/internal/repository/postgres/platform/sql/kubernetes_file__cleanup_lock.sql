-- name: kubernetes_file__cleanup_lock :one
SELECT source.id::text FROM control_plane.kubernetes_file_input_sources source
WHERE source.id=@source_id::uuid AND source.organization_id=@organization_id::uuid FOR UPDATE;
