-- name: kubernetes_setup__fact_classification :one
SELECT EXISTS(SELECT 1 FROM control_plane.kubernetes_identity_acknowledgement_rejections rejection
 JOIN control_plane.kubernetes_identity_upload_attempts attempt ON attempt.id=rejection.attempt_id
 WHERE attempt.setup_id=@setup_id::uuid AND attempt.organization_id=@organization_id::uuid),
 EXISTS(SELECT 1 FROM control_plane.kubernetes_identity_upload_observations observation
 JOIN control_plane.kubernetes_identity_upload_attempts attempt ON attempt.id=observation.attempt_id
 WHERE attempt.setup_id=@setup_id::uuid AND attempt.organization_id=@organization_id::uuid AND observation.outcome='KNOWN');
