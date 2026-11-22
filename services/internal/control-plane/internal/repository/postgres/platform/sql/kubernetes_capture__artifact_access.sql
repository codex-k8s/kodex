-- name: kubernetes_capture__artifact_access :one
SELECT control_plane.kubernetes_capture_artifact_visible(@organization_id::uuid,@actor_id::uuid,@artifact_id::uuid)
 AND (@permission<>'artifact.revision.create' OR NOT EXISTS(
  SELECT 1 FROM control_plane.kubernetes_capture_artifact_heads
  WHERE organization_id=@organization_id::uuid AND artifact_id=@artifact_id::uuid));
