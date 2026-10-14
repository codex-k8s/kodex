-- name: prepared_content_writer_lock :one
SELECT id::text FROM control_plane.prepared_content
WHERE id=@id AND state='IN_FLIGHT' AND generation=1 AND writer_deadline>clock_timestamp()
FOR UPDATE;
