-- name: kubernetes_file__source_failure :exec
UPDATE control_plane.kubernetes_file_input_sources SET state=@state,reason_code=@reason
WHERE id=@source_id::uuid AND organization_id=@organization_id::uuid AND state='PREPARING';
