DELETE FROM control_plane.artifact_revisions WHERE artifact_id=(SELECT id FROM control_plane.artifact_heads WHERE ref=$1);
