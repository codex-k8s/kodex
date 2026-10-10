UPDATE control_plane.artifact_revisions SET file_name='rewritten' WHERE artifact_id=(SELECT id FROM control_plane.artifact_heads WHERE ref=$1);
