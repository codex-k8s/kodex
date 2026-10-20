-- name: artifact_revision_get :one
SELECT revision.ref,artifact.ref,revision.revision,revision.file_name,revision.media_type,
       revision.size_bytes,revision.digest,control_plane.artifact_effective_scan(revision.id),revision.source,
       revision.preview_state='AVAILABLE' AND control_plane.artifact_effective_scan(revision.id)='CLEAN',revision.created_at
FROM control_plane.artifacts artifact
JOIN control_plane.artifact_revisions revision ON revision.artifact_id=artifact.id
WHERE artifact.organization_id=@organization_id::uuid AND artifact.ref=@artifact_ref
  AND revision.ref=@revision_ref AND artifact.lifecycle_state='ACTIVE';
