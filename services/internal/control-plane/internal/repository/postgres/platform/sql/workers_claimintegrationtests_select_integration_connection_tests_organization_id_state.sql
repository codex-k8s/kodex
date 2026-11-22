-- name: workers_claimintegrationtests_select_integration_connection_tests_organization_id_state :many
SELECT t.id::text,t.ref,t.generation,c.ref,c.definition_key,c.public_configuration,
 c.definition_version,c.definition_digest,
 COALESCE(cr.ref,''),COALESCE(cr.revision,0),COALESCE(cr.secret_ref,''),COALESCE(cr.secret_uid::text,''),
 COALESCE(cr.secret_resource_version,''),COALESCE(cr.content_sha256,''),cr.created_at,
 t.credential_kind,t.input_digest,COALESCE(t.broker_identity_revision_id::text,''),c.credential_revision_id IS NOT NULL,
 COALESCE(identity.ref,''),COALESCE(identity.revision,0),COALESCE(identity.purpose,''),COALESCE(identity.target_ref,''),identity.created_at,
 COALESCE(subject.ref,''),COALESCE(subject.active AND subject.kind='SERVICE' AND identity.binding_id IS NULL AND identity.credential_policy='PROJECT_SHARED',false)
FROM control_plane.integration_connection_tests t
JOIN control_plane.integration_connections c ON c.id=t.connection_id AND c.organization_id=t.organization_id
JOIN control_plane.integration_definitions d ON d.stable_key=c.definition_key
LEFT JOIN control_plane.integration_credential_revisions cr ON cr.id=c.credential_revision_id AND cr.organization_id=c.organization_id
LEFT JOIN control_plane.integration_broker_identity_revisions identity ON identity.id=t.broker_identity_revision_id
 AND identity.organization_id=t.organization_id AND identity.connection_id=c.id
LEFT JOIN control_plane.subjects subject ON subject.id=identity.subject_id AND subject.organization_id=t.organization_id
WHERE t.organization_id=$1::uuid AND t.state='DUE' AND c.enabled
 AND ((t.purpose='OWNER_TEST' AND c.state='TESTING') OR
      (t.purpose='MANAGED_MCP_REFRESH' AND $3='integration-gateway'
       AND control_plane.context7_health_refresh_eligible(t.organization_id,c.id)))
 AND t.input_snapshot<>'{}'::jsonb
 AND (c.definition_key<>'email' OR (t.credential_kind='NONE'
  AND c.credential_revision_id IS NULL AND c.broker_identity_revision_id IS NULL
  AND control_plane.email_connection_test_mailbox_current(t.id)))
 AND t.input_digest=encode(sha256(convert_to(t.input_snapshot::text,'UTF8')),'hex')
 AND t.input_snapshot->>'connectionRef'=c.ref
 AND (t.input_snapshot->>'connectionVersion')::bigint=c.version
 AND t.input_snapshot->'configuration'=c.public_configuration
 AND t.input_snapshot->>'definitionVersion'=c.definition_version
 AND t.input_snapshot->>'definitionDigest'=c.definition_digest
 AND ((t.credential_kind IN('NONE','TOKEN') AND t.broker_purpose IS NULL AND t.broker_identity_revision_id IS NULL
       AND c.broker_identity_revision_id IS NULL
       AND t.input_snapshot->>'credentialRevisionRef'=COALESCE(cr.ref,'')
       AND t.input_snapshot->>'credentialSHA256'=COALESCE(cr.content_sha256,''))
  OR (t.credential_kind='BROKER_IDENTITY' AND t.purpose='OWNER_TEST' AND t.attempt=1 AND t.generation=0
       AND c.credential_revision_id IS NULL AND c.credential_policy='PROJECT_SHARED'
       AND c.broker_identity_revision_id=t.broker_identity_revision_id AND identity.purpose=t.broker_purpose
       AND identity.binding_id IS NULL AND identity.credential_policy='PROJECT_SHARED'
       AND t.input_snapshot->>'credentialDeliveryKind'='BROKER_IDENTITY'
       AND t.input_snapshot->>'brokerIdentityRef'=identity.ref
       AND (t.input_snapshot->>'brokerIdentityRevision')::bigint=identity.revision
       AND t.input_snapshot->>'brokerIdentityPurpose'=identity.purpose
       AND t.input_snapshot->>'brokerIdentityTargetRef'=identity.target_ref
       AND t.input_snapshot->>'serviceSubjectRef'=subject.ref
       AND identity.target_ref=c.public_configuration->>'target_ref'
       AND subject.ref=c.public_configuration->>'service_subject_ref' AND subject.active AND subject.kind='SERVICE'))
 AND (t.purpose='MANAGED_MCP_REFRESH' OR t.attempt=1 OR t.updated_at<=clock_timestamp()-INTERVAL '5 seconds')
 AND d.enabled AND d.adapter_owner=$3 AND d.execution_route=$4 AND d.adapter_readiness='READY'
ORDER BY t.created_at
FOR UPDATE OF t SKIP LOCKED
LIMIT $2
