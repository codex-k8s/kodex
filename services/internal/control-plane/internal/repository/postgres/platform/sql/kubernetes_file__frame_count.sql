-- name: kubernetes_file__frame_count :one
SELECT count(*) FROM control_plane.kubernetes_file_input_frames WHERE source_id=@source_id::uuid AND organization_id=@organization_id::uuid;
