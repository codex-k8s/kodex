-- name: prepared_content_adopt :exec
UPDATE control_plane.prepared_content content
SET state='ADOPTED',adopted_revision_id=revision.id,adopted_revision_ref=revision.ref,
 generation=content.generation+1,updated_at=clock_timestamp()
FROM control_plane.artifact_revisions revision
JOIN control_plane.artifact_heads head ON head.id=revision.artifact_id
JOIN control_plane.artifact_revision_content receipt ON receipt.revision_id=revision.id
WHERE content.id=@id AND content.organization_id=@organization_id AND revision.id=@revision_id
 AND head.organization_id=content.organization_id AND head.project_id=content.project_id
 AND head.ref=content.prepared_artifact_ref
 AND (content.target_artifact_id IS NULL OR content.target_artifact_id=head.id)
 AND (content.target_artifact_id IS NULL OR revision.ref=content.prepared_revision_ref)
 AND revision.digest=content.digest AND revision.size_bytes=content.size_bytes
 AND revision.file_name=content.file_name AND revision.media_type=content.media_type AND revision.scan_state='CLEAN'
 AND receipt.object_key=content.object_key AND receipt.object_version=content.object_version
 AND receipt.object_etag=content.object_etag AND receipt.digest=content.digest AND receipt.size_bytes=content.size_bytes
 AND content.state='STAGED' AND content.plan_revision_id IS NOT NULL AND content.available_until>clock_timestamp();
