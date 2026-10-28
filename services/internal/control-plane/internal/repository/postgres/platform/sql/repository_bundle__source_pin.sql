-- name: repository_bundle__source_pin :one
SELECT revision.ref,content.object_key,content.object_version,content.object_etag,content.digest,content.size_bytes
FROM control_plane.artifact_heads head
JOIN control_plane.artifact_revisions revision ON revision.artifact_id=head.id
JOIN control_plane.artifact_revision_content content ON content.revision_id=revision.id
  AND content.digest=revision.digest AND content.size_bytes=revision.size_bytes
WHERE head.organization_id=@organization_id::uuid AND head.project_id=@project_id::uuid
  AND head.ref=@artifact_ref AND head.lifecycle_state='ACTIVE'
  AND revision.revision=@revision AND revision.digest='sha256:'||@sha256
  AND revision.size_bytes=@size_bytes AND control_plane.artifact_effective_scan(revision.id)='CLEAN'
FOR SHARE OF head NOWAIT;
