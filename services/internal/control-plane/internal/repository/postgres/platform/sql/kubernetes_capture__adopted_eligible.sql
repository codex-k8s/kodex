-- name: kubernetes_capture__adopted_eligible :one
SELECT count(*)>0 AND bool_and(head.lifecycle_state='ACTIVE' AND control_plane.artifact_effective_scan(revision.id) IN('CLEAN','NOT_SCANNED')
 AND revision.digest=saved.digest AND revision.size_bytes=saved.size_bytes
 AND control_plane.kubernetes_capture_artifact_visible(saved.organization_id,@actor_id::uuid,saved.artifact_id))
FROM control_plane.kubernetes_capture_artifact_revisions saved
JOIN control_plane.artifact_heads head ON head.id=saved.artifact_id AND head.organization_id=saved.organization_id
JOIN control_plane.artifact_revisions revision ON revision.id=saved.revision_id AND revision.artifact_id=head.id
WHERE saved.organization_id=@organization_id::uuid AND saved.capture_id=@capture_id::uuid;
