-- name: kubernetes_capture__artifact_head :one
SELECT head.id::text,head.ref,head.version,revision.ref,revision.revision,head.lifecycle_state
FROM control_plane.artifact_heads head
JOIN control_plane.artifact_revisions revision ON revision.id=head.current_revision_id AND revision.artifact_id=head.id
WHERE head.id=@artifact_id::uuid AND head.organization_id=@organization_id::uuid
FOR UPDATE OF head;
