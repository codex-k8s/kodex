-- name: runtime_files_metadata :one
SELECT entry.ref,entry.artifact_ref,entry.artifact_revision,entry.artifact_version,entry.artifact_digest,
    entry.file_name,entry.media_type,entry.size_bytes,entry.purpose,entry.project_ref,entry.run_ref,
    entry.source,entry.source_ref,entry.source_revision_ref,
    content.object_key,content.object_version,content.object_etag,content.digest,content.size_bytes
FROM control_plane.runtime_file_visible_entries entry
JOIN control_plane.artifact_content content ON content.artifact_id=entry.artifact_id
WHERE entry.catalog_id=@catalog_id::uuid AND entry.purpose=@purpose AND entry.ref=@entry_ref
  AND entry.artifact_ref=@artifact_ref AND entry.artifact_revision=@revision AND entry.artifact_digest=@digest;
