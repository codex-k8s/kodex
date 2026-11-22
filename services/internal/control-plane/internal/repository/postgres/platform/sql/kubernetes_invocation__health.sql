-- name: kubernetes_invocation__health :one
SELECT attempt.id::text,completion.subject_digest
FROM control_plane.kubernetes_identity_health_attempts attempt
JOIN control_plane.kubernetes_identity_health_completions completion ON completion.attempt_id=attempt.id
 AND completion.state='SUCCEEDED' AND completion.outcome='AUTHENTICATED' AND completion.applied
LEFT JOIN control_plane.integration_personal_credential_tests test ON test.id=attempt.personal_test_id
 AND test.organization_id=attempt.organization_id
LEFT JOIN control_plane.integration_connection_tests shared_test ON shared_test.id=attempt.shared_test_id AND shared_test.organization_id=attempt.organization_id
WHERE attempt.organization_id=@organization_id::uuid AND attempt.identity_revision_id=@identity_id::uuid
 AND attempt.registry_revision=@registry_revision AND attempt.target_ref=@target_ref AND attempt.target_revision=@target_revision
 AND ((test.id IS NOT NULL AND test.input_snapshot->>'DefinitionDigest'=@definition_digest
  AND test.input_snapshot->>'ConfigurationDigest'=@configuration_digest)
 OR (shared_test.id IS NOT NULL AND shared_test.state='SUCCEEDED' AND shared_test.credential_kind='BROKER_IDENTITY'
  AND shared_test.broker_purpose='KUBERNETES_TARGET' AND shared_test.broker_identity_revision_id=attempt.identity_revision_id
  AND shared_test.input_snapshot->>'definitionDigest'=@definition_digest
  AND shared_test.input_snapshot->'configuration'=@configuration::jsonb))
ORDER BY completion.created_at DESC,attempt.ref DESC LIMIT 1;
