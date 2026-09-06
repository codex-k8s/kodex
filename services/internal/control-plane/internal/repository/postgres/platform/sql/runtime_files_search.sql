-- name: runtime_files_search :many
SELECT entry.ref,entry.artifact_ref,entry.artifact_revision,entry.artifact_version,entry.artifact_digest,
    entry.file_name,entry.media_type,entry.size_bytes,entry.purpose,entry.project_ref,entry.run_ref,
    entry.source,entry.source_ref,entry.source_revision_ref
FROM control_plane.runtime_file_visible_entries entry
WHERE entry.catalog_id=@catalog_id::uuid AND entry.purpose=@purpose
  AND (@query='' OR strpos(lower(entry.file_name),lower(@query))>0)
  AND entry.ref>@after_ref ORDER BY entry.ref LIMIT @page_limit;
