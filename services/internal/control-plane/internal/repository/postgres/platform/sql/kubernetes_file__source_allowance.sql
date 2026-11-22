-- name: kubernetes_file__source_allowance :exec
UPDATE control_plane.kubernetes_file_input_sources SET authorized_until=@authorized_until,source_access_until=@source_access_until
WHERE id=@source_id::uuid AND organization_id=@organization_id::uuid AND state='READY' AND payload_purged_at IS NULL;
