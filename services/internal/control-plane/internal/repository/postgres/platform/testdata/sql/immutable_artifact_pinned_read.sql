SELECT content.object_key,content.object_version,artifact.file_name,artifact.digest,item.artifact_version
FROM control_plane.attachment_sets attachment
JOIN control_plane.attachment_set_items item ON item.attachment_set_id=attachment.id
JOIN control_plane.artifact_history artifact ON artifact.id=item.artifact_id AND artifact.ref=item.artifact_ref
 AND artifact.revision=item.artifact_revision AND artifact.digest=item.digest AND artifact.file_name=item.file_name
 AND artifact.media_type=item.media_type AND artifact.size_bytes=item.size_bytes AND artifact.source=item.source
JOIN control_plane.artifact_revision_content content ON content.revision_id=artifact.revision_id
 AND content.digest=item.digest AND content.size_bytes=item.size_bytes
WHERE attachment.ref=$1 AND artifact.lifecycle_state='ACTIVE' AND artifact.scan_state='CLEAN'
 AND artifact.organization_id=attachment.organization_id AND artifact.project_id IS NOT DISTINCT FROM attachment.project_id
 AND artifact.revision=$2 AND artifact.digest=$3;
