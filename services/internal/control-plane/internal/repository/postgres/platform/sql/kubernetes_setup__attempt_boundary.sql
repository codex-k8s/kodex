-- name: kubernetes_setup__attempt_boundary :one
SELECT EXISTS(SELECT 1 FROM control_plane.kubernetes_identity_upload_attempts
 WHERE organization_id=@organization_id::uuid AND setup_id=@setup_id::uuid AND state='ACTIVE'),
 EXISTS(SELECT 1 FROM control_plane.kubernetes_identity_upload_attempts
 WHERE organization_id=@organization_id::uuid AND setup_id=@setup_id::uuid AND mode='WRITE'),
 NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_identity_upload_attempts attempt
 WHERE attempt.organization_id=@organization_id::uuid AND attempt.setup_id=@setup_id::uuid
  AND NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_identity_upload_observations observation
   WHERE observation.attempt_id=attempt.id))
 AND NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_identity_acknowledgement_rejections rejection
  JOIN control_plane.kubernetes_identity_upload_attempts attempt ON attempt.id=rejection.attempt_id
  WHERE attempt.organization_id=@organization_id::uuid AND attempt.setup_id=@setup_id::uuid);
