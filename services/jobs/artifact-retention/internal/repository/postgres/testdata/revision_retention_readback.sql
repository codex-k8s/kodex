SELECT head.lifecycle_state,head.current_revision_id IS NULL,
 (SELECT count(*) FROM control_plane.artifact_revision_content content JOIN control_plane.artifact_revisions revision ON revision.id=content.revision_id WHERE revision.artifact_id=head.id),
 (SELECT count(*) FROM control_plane.artifact_revisions revision WHERE revision.artifact_id=head.id),
 (SELECT count(*) FROM control_plane.artifact_bindings binding WHERE binding.artifact_id=head.id AND binding.artifact_revision=7 AND (binding.revision_id IS NULL)=$1),
 (SELECT count(*) FROM control_plane.artifact_download_grants grant_row WHERE grant_row.artifact_id=head.id AND grant_row.artifact_revision=7 AND (grant_row.revision_id IS NULL)=$1)
FROM control_plane.artifact_heads head WHERE head.ref='art_retention_fixture';
