-- name: shared_health_test__read :one
SELECT t.ref,t.credential_kind,COALESCE(t.broker_purpose,''),COALESCE(t.broker_identity_revision_id::text,''),t.input_snapshot::text,t.input_digest,t.state,
 t.input_snapshot=jsonb_build_object('connectionRef',c.ref,'connectionVersion',c.version,'configuration',c.public_configuration,
  'definitionKey',c.definition_key,'definitionVersion',c.definition_version,'definitionDigest',c.definition_digest,
  'credentialRevisionRef',COALESCE(cr.ref,''),'credentialRevision',COALESCE(cr.revision,0),'credentialSHA256',COALESCE(cr.content_sha256,''))
FROM control_plane.integration_connection_tests t
JOIN control_plane.integration_connections c ON c.id=t.connection_id AND c.organization_id=t.organization_id
LEFT JOIN control_plane.integration_credential_revisions cr ON cr.id=c.credential_revision_id AND cr.organization_id=c.organization_id
WHERE t.organization_id=$1::uuid AND c.ref=$2 ORDER BY t.created_at DESC LIMIT 1;
