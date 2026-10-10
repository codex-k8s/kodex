-- name: artifact_revision_create_content :exec
INSERT INTO control_plane.artifact_revision_content(revision_id,object_key,object_version,object_etag,digest,size_bytes)
VALUES($1::uuid,$2,$3,$4,$5,$6)
