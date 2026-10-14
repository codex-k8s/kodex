-- name: runtime_files_metadata :one
SELECT entry.ref,entry.artifact_ref,entry.artifact_revision,entry.artifact_version,entry.artifact_digest,
    entry.file_name,entry.media_type,entry.size_bytes,entry.purpose,entry.project_ref,entry.run_ref,
    entry.source,entry.source_ref,entry.source_revision_ref,
    content.object_key,content.object_version,content.object_etag,content.digest,content.size_bytes
FROM control_plane.runtime_file_visible_entries entry
JOIN control_plane.artifact_history artifact ON artifact.id=entry.artifact_id
  AND artifact.ref=entry.artifact_ref AND artifact.revision=entry.artifact_revision
  AND artifact.digest=entry.artifact_digest AND artifact.size_bytes=entry.size_bytes
  AND artifact.media_type=entry.media_type AND artifact.source=entry.source
JOIN control_plane.artifact_revision_content content ON content.revision_id=artifact.revision_id
  AND content.digest=entry.artifact_digest AND content.size_bytes=entry.size_bytes
WHERE entry.catalog_id=@catalog_id::uuid AND entry.purpose=@purpose AND entry.ref=@entry_ref
  AND entry.artifact_ref=@artifact_ref AND entry.artifact_revision=@revision AND entry.artifact_digest=@digest;
