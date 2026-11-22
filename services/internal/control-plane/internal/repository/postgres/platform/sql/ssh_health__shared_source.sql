-- name: ssh_health__shared_source :one
SELECT COALESCE(c.enabled AND c.state='TESTING' AND c.credential_policy='PROJECT_SHARED' AND c.credential_revision_id IS NULL
 AND c.broker_identity_revision_id=t.broker_identity_revision_id AND t.credential_kind='BROKER_IDENTITY' AND t.broker_purpose='SSH_TARGET'
 AND t.purpose='OWNER_TEST' AND t.attempt=1 AND t.generation=1 AND t.claimed_workload='integration-gateway'
 AND t.input_digest=encode(sha256(convert_to(t.input_snapshot::text,'UTF8')),'hex')
 AND t.input_snapshot=jsonb_build_object('connectionRef',c.ref,'connectionVersion',c.version,'configuration',c.public_configuration,
 'definitionKey',c.definition_key,'definitionVersion',c.definition_version,'definitionDigest',c.definition_digest,
 'credentialDeliveryKind','BROKER_IDENTITY','brokerIdentityPurpose',i.purpose,'brokerIdentityRef',i.ref,'brokerIdentityRevision',i.revision,
 'brokerIdentityTargetRef',i.target_ref,'serviceSubjectRef',subject.ref)
 AND subject.active AND subject.kind='SERVICE' AND i.binding_id IS NULL AND i.credential_policy='PROJECT_SHARED'
 AND subject.ref=c.public_configuration->>'service_subject_ref' AND i.target_ref=c.public_configuration->>'target_ref',false)
FROM control_plane.integration_connection_tests t JOIN control_plane.integration_connections c ON c.id=t.connection_id AND c.organization_id=t.organization_id
LEFT JOIN control_plane.integration_broker_identity_revisions i ON i.id=t.broker_identity_revision_id AND i.organization_id=t.organization_id AND i.connection_id=c.id
LEFT JOIN control_plane.subjects subject ON subject.id=i.subject_id AND subject.organization_id=i.organization_id
WHERE t.organization_id=@organization_id::uuid AND t.ref=@ref;
