-- name: kubernetes_health__material :one
SELECT identity.id::text,material.secret_namespace,material.secret_name,material.secret_uid::text,
 material.secret_resource_version,material.content_sha256
FROM control_plane.integration_broker_identity_revisions identity
JOIN control_plane.kubernetes_identity_material_revisions material ON material.identity_revision_id=identity.id
 AND material.organization_id=identity.organization_id AND material.purpose=identity.purpose
WHERE identity.organization_id=@organization_id::uuid AND identity.ref=@identity_ref
 AND identity.binding_id=@binding_id::uuid AND identity.subject_id=@subject_id::uuid
 AND identity.purpose='KUBERNETES_TARGET' AND identity.target_ref=@target_ref
 AND identity.revision=@revision AND identity.credential_policy='INITIATOR_REQUIRED'
 AND NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_identity_setups setup
  JOIN control_plane.kubernetes_identity_upload_attempts attempt ON attempt.setup_id=setup.id
  JOIN control_plane.kubernetes_identity_acknowledgement_rejections rejection ON rejection.attempt_id=attempt.id
  WHERE setup.identity_revision_id=identity.id);
