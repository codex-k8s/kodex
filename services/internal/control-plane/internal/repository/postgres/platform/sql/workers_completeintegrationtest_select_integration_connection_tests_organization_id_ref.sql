-- name: workers_completeintegrationtest_select_integration_connection_tests_organization_id_ref :one
SELECT t.id::text,t.connection_id::text,c.ref,t.fence_digest,t.generation,t.state,t.lease_ref,t.lease_expires_at,
       t.attempt,t.created_at,c.definition_key,c.state,c.enabled,t.purpose
FROM control_plane.integration_connection_tests t
JOIN control_plane.integration_connections c ON c.id=t.connection_id
WHERE t.organization_id=$1::uuid AND t.ref=$2 AND t.claimed_workload=$3
  AND (t.purpose='OWNER_TEST' OR (
    control_plane.context7_health_refresh_eligible(t.organization_id,c.id)
    AND t.input_snapshot->>'connectionVersion'=c.version::text
    AND t.input_snapshot->'configuration'=c.public_configuration
    AND t.input_snapshot->>'definitionVersion'=c.definition_version
    AND t.input_snapshot->>'definitionDigest'=c.definition_digest
    AND EXISTS (SELECT 1 FROM control_plane.integration_credential_revisions cr
                WHERE cr.id=c.credential_revision_id AND cr.organization_id=c.organization_id
                  AND cr.ref=t.input_snapshot->>'credentialRevisionRef'
                  AND cr.revision::text=t.input_snapshot->>'credentialRevision'
                  AND cr.content_sha256=t.input_snapshot->>'credentialSHA256')
  ))
FOR UPDATE OF t,c
