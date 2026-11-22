-- name: broker_identity__kubernetes_fact :one
SELECT max(least(setup.expires_at,attempt.expires_at))
 FROM control_plane.kubernetes_identity_setups setup
 JOIN control_plane.kubernetes_identity_material_revisions material
  ON material.identity_revision_id=setup.identity_revision_id AND material.organization_id=setup.organization_id
   AND material.content_sha256=setup.content_sha256
 JOIN control_plane.kubernetes_identity_upload_attempts attempt ON attempt.setup_id=setup.id
  AND attempt.organization_id=setup.organization_id AND attempt.state='COMPLETED'
 JOIN control_plane.kubernetes_identity_upload_observations observation ON observation.attempt_id=attempt.id
  AND observation.outcome='KNOWN' AND observation.identity_revision_id=setup.identity_revision_id
 JOIN control_plane.kubernetes_operator_current current ON current.revision=setup.registry_revision
 JOIN control_plane.kubernetes_operator_registry_revisions registry ON registry.revision=current.revision
 WHERE setup.identity_revision_id=@identity_id::uuid AND setup.organization_id=@organization_id::uuid
  AND setup.connection_id=@connection_id::uuid AND setup.state='COMPLETED'
  AND setup.expires_at>clock_timestamp() AND attempt.expires_at>clock_timestamp()
  AND material.secret_namespace=registry.identity_namespace
  AND NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_identity_acknowledgement_rejections rejection
   JOIN control_plane.kubernetes_identity_upload_attempts rejected_attempt ON rejected_attempt.id=rejection.attempt_id
   WHERE rejected_attempt.setup_id=setup.id)
;
