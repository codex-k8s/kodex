-- name: workflow_evidence__content :one
SELECT history.ref,history.revision_ref,history.revision,history.digest,history.file_name,history.media_type,history.size_bytes,
    content.object_key,content.object_version,content.object_etag
FROM control_plane.artifact_history history
JOIN control_plane.artifact_revision_content content ON content.revision_id=history.revision_id
    AND content.digest=history.digest AND content.size_bytes=history.size_bytes
WHERE history.organization_id=@organization_id::uuid AND history.project_id=@project_id::uuid
    AND history.ref=@artifact_ref AND history.revision_ref=@revision_ref AND history.digest=@artifact_digest;
