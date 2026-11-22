-- name: kubernetes_file__source_manifest :exec
UPDATE control_plane.kubernetes_file_input_sources SET payload_digest=@payload_digest,byte_count=@byte_count,
 normalization_revision=@normalization_revision,manifest=@manifest,manifest_digest=@manifest_digest,authorized_until=@authorized_until,source_access_until=@source_access_until
WHERE id=@source_id::uuid AND organization_id=@organization_id::uuid AND state='PREPARING' AND payload_digest IS NULL
 AND writer_deadline>clock_timestamp();
