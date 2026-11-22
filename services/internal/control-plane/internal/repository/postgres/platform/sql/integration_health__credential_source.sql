-- name: integration_health__credential_source :one
SELECT c.public_configuration,c.credential_revision_id IS NOT NULL,COALESCE(c.broker_identity_revision_id::text,''),
 COALESCE(identity.ref,''),COALESCE(identity.revision,0),COALESCE(identity.purpose,''),COALESCE(identity.target_ref,''),identity.created_at,
 COALESCE(subject.ref,''),COALESCE(subject.active AND subject.kind='SERVICE' AND identity.binding_id IS NULL AND identity.credential_policy='PROJECT_SHARED',false)
FROM control_plane.integration_connections c
LEFT JOIN control_plane.integration_broker_identity_revisions identity ON identity.id=c.broker_identity_revision_id
 AND identity.organization_id=c.organization_id AND identity.connection_id=c.id
LEFT JOIN control_plane.subjects subject ON subject.id=identity.subject_id AND subject.organization_id=c.organization_id
WHERE c.organization_id=@organization_id::uuid AND c.id=@connection_id::uuid
 AND c.lifecycle_state='ACTIVE'
 AND (c.credential_policy='PROJECT_SHARED' OR
  (@allow_personal_email::boolean AND c.definition_key='email' AND c.credential_policy='INITIATOR_REQUIRED'
   AND c.credential_revision_id IS NULL AND c.broker_identity_revision_id IS NULL));
