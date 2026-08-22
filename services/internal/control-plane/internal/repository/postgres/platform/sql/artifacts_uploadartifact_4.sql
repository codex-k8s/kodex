-- name: platform__artifacts_uploadartifact_4 :exec
INSERT INTO control_plane.artifact_content(artifact_id,body) SELECT id,$2 FROM control_plane.artifacts WHERE ref=$1
