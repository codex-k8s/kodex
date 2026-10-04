-- name: runtime_managed_mcp__health :one
SELECT t.ref, t.generation, c.ref, c.version, c.public_configuration,
       cr.ref, cr.revision, cr.content_sha256, c.definition_key,
       c.definition_version, c.definition_digest, t.completed_at
FROM control_plane.integration_connection_tests t
JOIN control_plane.integration_connections c
  ON c.id=t.connection_id AND c.organization_id=t.organization_id
JOIN control_plane.integration_credential_revisions cr
  ON cr.id=c.credential_revision_id AND cr.organization_id=c.organization_id
WHERE c.organization_id=@organization_id::uuid AND c.ref=@connection_ref
  AND c.enabled AND c.state='CONNECTED' AND c.definition_key='context7'
  AND c.version=@connection_version AND c.definition_version=@definition_version
  AND c.definition_digest=@definition_digest
  AND t.state='SUCCEEDED' AND t.generation > 0 AND t.completed_at IS NOT NULL
  AND t.completed_at BETWEEN clock_timestamp()-INTERVAL '5 minutes' AND clock_timestamp()
  AND t.claimed_workload='integration-gateway'
  AND t.input_snapshot->>'connectionRef'=c.ref
  AND t.input_snapshot->'configuration'=c.public_configuration
  AND t.input_snapshot->>'definitionKey'=c.definition_key
  AND t.input_snapshot->>'definitionVersion'=c.definition_version
  AND t.input_snapshot->>'definitionDigest'=c.definition_digest
  AND t.input_snapshot->>'credentialRevisionRef'=cr.ref
  AND (t.input_snapshot->>'credentialRevision')::bigint=cr.revision
  AND t.input_snapshot->>'credentialSHA256'=cr.content_sha256
ORDER BY t.completed_at DESC, t.ref LIMIT 1
FOR SHARE OF t,c,cr
