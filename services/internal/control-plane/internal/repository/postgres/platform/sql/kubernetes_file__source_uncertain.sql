-- name: kubernetes_file__source_uncertain :exec
UPDATE control_plane.kubernetes_file_input_sources SET state='UNKNOWN',reason_code='SOURCE_UNAVAILABLE'
WHERE id=@source_id::uuid AND organization_id=@organization_id::uuid AND state='PREPARING';
