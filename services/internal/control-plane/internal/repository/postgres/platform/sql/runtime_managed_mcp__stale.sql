-- name: runtime_managed_mcp__stale :one
-- Новый cycle разрешён только после последнего terminal success со stable inputs.
-- Любой active task закрывает этот origin: его 30s бюджет не сбрасывается.
SELECT t.ref, t.generation, c.ref, c.version, c.public_configuration,
       cr.ref, cr.revision, cr.content_sha256, c.definition_key,
       c.definition_version, c.definition_digest, t.completed_at
FROM control_plane.integration_connections c
JOIN control_plane.integration_credential_revisions cr
  ON cr.id=c.credential_revision_id AND cr.organization_id=c.organization_id
JOIN LATERAL (
  SELECT latest.* FROM control_plane.integration_connection_tests latest
  WHERE latest.organization_id=c.organization_id AND latest.connection_id=c.id
    AND latest.completed_at IS NOT NULL
  ORDER BY latest.completed_at DESC,latest.ref DESC LIMIT 1
) t ON true
WHERE c.organization_id=@organization_id::uuid AND c.ref=@connection_ref
  AND c.enabled AND c.lifecycle_state='ACTIVE' AND c.state='CONNECTED' AND c.definition_key='context7'
  AND c.version=@connection_version AND c.definition_version=@definition_version
  AND c.definition_digest=@definition_digest
  AND control_plane.context7_health_refresh_eligible(c.organization_id,c.id)
  AND t.state='SUCCEEDED' AND t.generation>0 AND t.claimed_workload='integration-gateway'
  AND t.completed_at<=clock_timestamp()-INTERVAL '5 minutes'
  AND t.input_snapshot->>'connectionRef'=c.ref
  AND t.input_snapshot->'configuration'=c.public_configuration
  AND t.input_snapshot->>'definitionKey'=c.definition_key
  AND t.input_snapshot->>'definitionVersion'=c.definition_version
  AND t.input_snapshot->>'definitionDigest'=c.definition_digest
  AND t.input_snapshot->>'credentialRevisionRef'=cr.ref
  AND (t.input_snapshot->>'credentialRevision')::bigint=cr.revision
  AND t.input_snapshot->>'credentialSHA256'=cr.content_sha256
  AND NOT EXISTS (SELECT 1 FROM control_plane.integration_connection_tests active
    WHERE active.organization_id=c.organization_id AND active.connection_id=c.id
      AND active.state IN ('DUE','CLAIMED'))
FOR SHARE OF c,cr;
