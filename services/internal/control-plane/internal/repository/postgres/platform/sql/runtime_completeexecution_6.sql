-- name: platform__runtime_completeexecution_6 :exec
INSERT INTO control_plane.artifact_content(artifact_id,body) VALUES($1::uuid,$2)
