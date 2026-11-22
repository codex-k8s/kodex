-- name: kubernetes_setup__private_view :one
SELECT setup.ref,connection.ref,identity.ref,identity.target_ref,setup.version,
 connection.version,coalesce(binding.version,0),coalesce(binding.generation,0),identity.revision,
 identity.credential_policy,setup.state,setup.expires_at,setup.expires_at<=clock_timestamp(),
 EXISTS(SELECT 1 FROM control_plane.kubernetes_identity_upload_attempts attempt WHERE attempt.setup_id=setup.id
  AND (attempt.state<>'ACTIVE' OR attempt.expires_at<=clock_timestamp()) AND NOT EXISTS(
   SELECT 1 FROM control_plane.kubernetes_identity_upload_observations observation WHERE observation.attempt_id=attempt.id)),
 EXISTS(SELECT 1 FROM control_plane.kubernetes_identity_acknowledgement_rejections rejection
  JOIN control_plane.kubernetes_identity_upload_attempts attempt ON attempt.id=rejection.attempt_id WHERE attempt.setup_id=setup.id),
 EXISTS(SELECT 1 FROM control_plane.kubernetes_identity_acknowledgement_rejections rejection
  JOIN control_plane.kubernetes_identity_upload_attempts attempt ON attempt.id=rejection.attempt_id
  WHERE attempt.setup_id=setup.id AND rejection.conflicts_with_known),
 target.payload::jsonb->'target'->>'embeddedContext'
FROM control_plane.kubernetes_identity_setups setup
JOIN control_plane.integration_connections connection ON connection.id=setup.connection_id AND connection.organization_id=setup.organization_id
JOIN control_plane.integration_broker_identity_revisions identity ON identity.id=setup.identity_revision_id AND identity.organization_id=setup.organization_id
JOIN control_plane.kubernetes_target_revisions target ON target.ref=setup.target_ref AND target.revision=setup.target_revision
LEFT JOIN control_plane.integration_personal_credentials binding ON binding.id=identity.binding_id
 AND binding.organization_id=identity.organization_id AND binding.connection_id=identity.connection_id AND binding.subject_id=identity.subject_id
WHERE setup.organization_id=@organization_id::uuid AND setup.id=@setup_id::uuid;
