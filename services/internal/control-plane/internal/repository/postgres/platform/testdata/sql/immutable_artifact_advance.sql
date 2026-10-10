UPDATE control_plane.artifact_heads SET current_revision_id=$2::uuid,version=version+1 WHERE ref=$1;
