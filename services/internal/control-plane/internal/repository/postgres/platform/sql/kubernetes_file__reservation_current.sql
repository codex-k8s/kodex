-- name: kubernetes_file__reservation_current :one
SELECT source.id::text FROM control_plane.kubernetes_file_input_sources source
WHERE source.organization_id=@organization_id::uuid AND source.ref=@source_ref
 AND source.state='PREPARING' AND source.payload_purged_at IS NULL AND source.deletion_receipt_digest IS NULL
 AND source.writer_completed_at IS NULL AND source.writer_deadline>clock_timestamp()
 AND source.authorized_until>clock_timestamp();
