-- name: platform__artifacts_uploadartifact_insert_artifact_content_artifact_id :exec
INSERT INTO control_plane.artifact_content(artifact_id,body) SELECT id,$2 FROM control_plane.artifacts WHERE ref=$1
