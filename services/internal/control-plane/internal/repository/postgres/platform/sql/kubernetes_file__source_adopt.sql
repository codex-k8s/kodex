-- name: kubernetes_file__source_adopt :exec
UPDATE control_plane.prepared_content SET state='ADOPTED',adopted_file_source_id=@source_id::uuid,updated_at=clock_timestamp()
WHERE id=@prepared_id::uuid AND organization_id=@organization_id::uuid AND source_profile='KUBERNETES_FILE_SOURCE'
 AND(state='STAGED' OR state='ADOPTED' AND adopted_file_source_id=@source_id::uuid);
