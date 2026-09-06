-- name: runtime_files_count :one
SELECT count(*) FROM control_plane.runtime_file_visible_entries entry
WHERE entry.catalog_id=@catalog_id::uuid AND entry.purpose=@purpose
  AND (@query='' OR strpos(lower(entry.file_name),lower(@query))>0);
