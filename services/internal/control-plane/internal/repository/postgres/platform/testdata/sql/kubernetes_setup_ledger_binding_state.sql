-- Синтетическая историческая READY metadata не доказывает package eligibility/RBAC.
-- Используется только для реального closing-only Revoke после смены package kind.
UPDATE control_plane.integration_personal_credentials binding
SET state=@state,version=binding.version+1,
 generation=binding.generation+CASE WHEN @state='CHECK_REQUIRED' THEN 1 ELSE 0 END,
 broker_identity_revision_id=setup.identity_revision_id,
 verified_broker_identity_revision_id=CASE WHEN @state='READY' THEN setup.identity_revision_id ELSE NULL END,
 verified_definition_digest=CASE WHEN @state='READY' THEN repeat('a',64) ELSE NULL END,
 verified_configuration_digest=CASE WHEN @state='READY' THEN repeat('b',64) ELSE NULL END
FROM control_plane.kubernetes_identity_setups setup
JOIN control_plane.integration_broker_identity_revisions identity ON identity.id=setup.identity_revision_id
 AND identity.organization_id=setup.organization_id
WHERE setup.id=@setup_id::uuid AND binding.id=identity.binding_id AND binding.organization_id=setup.organization_id;
