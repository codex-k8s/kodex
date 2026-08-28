-- name: runtime_completeexecution_insert_artifact_content_artifact_id :exec
INSERT INTO control_plane.artifact_content(
    artifact_id, object_key, object_version, object_etag, digest, size_bytes
)
VALUES($1::uuid, $2, $3, $4, $5, $6)
