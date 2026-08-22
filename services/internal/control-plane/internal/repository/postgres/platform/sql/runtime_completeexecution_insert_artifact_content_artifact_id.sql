-- name: runtime_completeexecution_insert_artifact_content_artifact_id :exec
INSERT INTO control_plane.artifact_content(artifact_id,body) VALUES($1::uuid,$2)
