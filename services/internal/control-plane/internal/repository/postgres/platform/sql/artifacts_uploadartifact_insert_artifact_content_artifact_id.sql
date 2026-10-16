-- name: artifacts_uploadartifact_insert_artifact_content_artifact_id :exec
INSERT INTO control_plane.artifact_content(
    artifact_id, object_key, object_version, object_etag, digest, size_bytes, antivirus_report
)
SELECT id, $2, $3, $4, $5, $6, $7
FROM control_plane.artifacts
WHERE ref = $1
