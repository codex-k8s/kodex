-- name: runtime_files_freeze_catalog :exec
UPDATE control_plane.runtime_file_catalogs SET frozen=true,digest=@digest,total=@total
WHERE id=@catalog_id::uuid AND NOT frozen;
