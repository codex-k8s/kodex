-- name: runtime_files_capture_digests :many
SELECT ref,entry_digest FROM control_plane.runtime_file_catalog_entries
WHERE catalog_id=@catalog_id::uuid ORDER BY ref;
