SELECT head.lifecycle_state,head.current_revision_id IS NULL,
 (SELECT count(*) FROM control_plane.artifact_revisions revision WHERE revision.artifact_id=head.id),
 (SELECT count(*) FROM control_plane.artifact_download_grants grant_row WHERE grant_row.artifact_id=head.id AND grant_row.revision_id IS NULL AND grant_row.artifact_revision_ref=$2),
 (SELECT count(*) FROM control_plane.attachment_set_items item WHERE item.artifact_id=head.id),
 (SELECT count(*) FROM control_plane.artifact_bindings binding WHERE binding.artifact_id=head.id AND binding.revision_id IS NULL AND binding.artifact_revision_ref=$2),
 artifact.file_name,artifact.digest
FROM control_plane.artifact_heads head JOIN control_plane.artifacts artifact ON artifact.id=head.id WHERE head.ref=$1;
