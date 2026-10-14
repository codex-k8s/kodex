-- name: skill_artifact_content :one
SELECT content.object_key,content.object_version,content.object_etag,content.digest,content.size_bytes
FROM control_plane.artifact_history artifact
JOIN control_plane.artifact_heads head ON head.id=artifact.id
JOIN control_plane.artifact_revision_content content ON content.revision_id=artifact.revision_id
WHERE artifact.organization_id=$1::uuid AND artifact.project_id=$2::uuid AND artifact.ref=$3 AND artifact.revision=$4
    AND artifact.lifecycle_state='ACTIVE' AND artifact.scan_state='CLEAN'
    AND content.digest=artifact.digest AND content.size_bytes=artifact.size_bytes
FOR SHARE OF head;
