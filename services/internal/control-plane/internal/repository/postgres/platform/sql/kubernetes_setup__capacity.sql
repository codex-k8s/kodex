-- name: kubernetes_setup__capacity :one
SELECT count(*)<4 AND NOT coalesce(bool_or(setup.connection_id=@connection_id::uuid
 AND setup.state IN('PREPARED','ATTEMPTING','UNKNOWN')),false)
FROM control_plane.kubernetes_identity_setups setup
WHERE setup.organization_id=@organization_id::uuid AND setup.actor_id=@actor_id::uuid
 AND (setup.state IN('PREPARED','ATTEMPTING','UNKNOWN') OR EXISTS(
  SELECT 1 FROM control_plane.kubernetes_identity_upload_attempts attempt
  JOIN control_plane.kubernetes_identity_upload_observations observation ON observation.attempt_id=attempt.id AND observation.outcome='UNKNOWN'
  WHERE attempt.setup_id=setup.id AND NOT EXISTS(
   SELECT 1 FROM control_plane.kubernetes_identity_upload_observations known
   JOIN control_plane.kubernetes_identity_upload_attempts known_attempt ON known_attempt.id=known.attempt_id
   WHERE known_attempt.setup_id=setup.id AND known.outcome='KNOWN'))
 OR EXISTS(
  SELECT 1 FROM control_plane.kubernetes_identity_material_revisions material
  WHERE material.identity_revision_id=setup.identity_revision_id
   AND NOT EXISTS(SELECT 1 FROM control_plane.integration_personal_credentials binding
    WHERE binding.broker_identity_revision_id=setup.identity_revision_id)
   AND NOT EXISTS(SELECT 1 FROM control_plane.integration_connections connection
    WHERE connection.broker_identity_revision_id=setup.identity_revision_id))
 OR EXISTS(SELECT 1 FROM control_plane.kubernetes_identity_acknowledgement_rejections rejection
  JOIN control_plane.kubernetes_identity_upload_attempts attempt ON attempt.id=rejection.attempt_id
  WHERE attempt.setup_id=setup.id));
