-- name: runtime_managed_mcp__pending :one
WITH RECURSIVE attempts AS (
    SELECT t.id,t.predecessor_ref,t.attempt,t.created_at,t.connection_id,t.organization_id
    FROM control_plane.integration_connection_tests t
    JOIN control_plane.integration_connections c ON c.id=t.connection_id AND c.organization_id=t.organization_id
    JOIN control_plane.integration_credential_revisions cr ON cr.id=c.credential_revision_id AND cr.organization_id=c.organization_id
    WHERE c.organization_id=@organization_id::uuid AND c.ref=@connection_ref
      AND c.version=@connection_version AND c.definition_version=@definition_version AND c.definition_digest=@definition_digest
      AND c.enabled AND c.lifecycle_state='ACTIVE' AND c.state='CONNECTED'
      AND control_plane.context7_health_refresh_eligible(c.organization_id,c.id)
      AND t.purpose='MANAGED_MCP_REFRESH' AND t.state IN ('DUE','CLAIMED')
      AND (t.state='DUE' OR (t.claimed_workload='integration-gateway' AND t.generation>0
           AND t.lease_ref IS NOT NULL AND t.fence_digest IS NOT NULL
           AND t.workload_instance IS NOT NULL AND t.lease_expires_at>clock_timestamp()))
      AND t.input_snapshot->>'connectionVersion'=c.version::text
      AND t.input_snapshot->'configuration'=c.public_configuration
      AND t.input_snapshot->>'definitionKey'=c.definition_key
      AND t.input_snapshot->>'definitionVersion'=c.definition_version
      AND t.input_snapshot->>'definitionDigest'=c.definition_digest
      AND t.input_snapshot->>'credentialRevisionRef'=cr.ref
      AND (t.input_snapshot->>'credentialRevision')::bigint=cr.revision
      AND t.input_snapshot->>'credentialSHA256'=cr.content_sha256
      AND EXISTS (
        SELECT 1 FROM control_plane.integration_grants resolve
        JOIN control_plane.integration_grants query ON query.connection_id=resolve.connection_id AND query.organization_id=resolve.organization_id
        JOIN control_plane.agents agent ON agent.organization_id=c.organization_id AND agent.ref=@agent_ref AND agent.enabled
        WHERE resolve.organization_id=c.organization_id AND resolve.connection_id=c.id
          AND resolve.target_kind='AGENT' AND query.target_kind='AGENT'
          AND resolve.target_ref=agent.ref AND query.target_ref=agent.ref
          AND resolve.ref=@resolve_ref AND resolve.version=@resolve_version
          AND query.ref=@query_ref AND query.version=@query_version
          AND resolve.enabled AND query.enabled AND resolve.risk='READ' AND query.risk='READ'
          AND resolve.approval_policy='NONE' AND query.approval_policy='NONE'
          AND resolve.capability_key='context7.library.resolve' AND query.capability_key='context7.docs.query'
          AND resolve.definition_version=c.definition_version AND query.definition_version=c.definition_version
          AND resolve.definition_digest=c.definition_digest AND query.definition_digest=c.definition_digest
      )
    UNION ALL
    SELECT predecessor.id,predecessor.predecessor_ref,predecessor.attempt,predecessor.created_at,predecessor.connection_id,predecessor.organization_id
    FROM attempts current
    JOIN control_plane.integration_connection_tests predecessor ON predecessor.ref=current.predecessor_ref
      AND predecessor.organization_id=current.organization_id AND predecessor.connection_id=current.connection_id
      AND predecessor.purpose='MANAGED_MCP_REFRESH' AND predecessor.attempt=current.attempt-1
    WHERE current.attempt>1
)
SELECT EXISTS (
    SELECT 1 FROM attempts HAVING count(*)>0 AND count(*)=max(attempt)
      AND min(created_at) BETWEEN clock_timestamp()-INTERVAL '30 seconds' AND clock_timestamp()
);
