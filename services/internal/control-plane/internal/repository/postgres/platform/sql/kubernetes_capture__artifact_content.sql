-- name: kubernetes_capture__artifact_content :exec
INSERT INTO control_plane.artifact_revision_content(revision_id,object_key,object_version,object_etag,digest,size_bytes,antivirus_report)
VALUES(@revision_id::uuid,@object_key,@object_version,@object_etag,@digest,@size_bytes,@antivirus_report::jsonb);
