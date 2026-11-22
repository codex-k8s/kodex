-- name: kubernetes_capture__origin :one
SELECT EXISTS(SELECT 1 FROM control_plane.kubernetes_capture_artifact_heads mapped WHERE mapped.artifact_id=head.id AND mapped.organization_id=head.organization_id),
 coalesce(saved.origin_json,''),coalesce(saved.source_digest,''),coalesce(capture.source_snapshot,''),coalesce(capture.snapshot_digest,''),coalesce(capture.capture_kind,'')
FROM control_plane.artifact_heads head
LEFT JOIN control_plane.artifact_revisions revision ON revision.artifact_id=head.id
 AND revision.ref=CASE WHEN @revision_ref='' THEN(SELECT current.ref FROM control_plane.artifact_revisions current WHERE current.id=head.current_revision_id) ELSE @revision_ref END
LEFT JOIN control_plane.kubernetes_capture_artifact_revisions saved ON saved.artifact_id=head.id AND saved.revision_id=revision.id AND saved.organization_id=head.organization_id
LEFT JOIN control_plane.kubernetes_output_captures capture ON capture.id=saved.capture_id AND capture.organization_id=saved.organization_id AND capture.state='COMPLETE'
WHERE head.organization_id=@organization_id::uuid AND head.ref=@artifact_ref;
