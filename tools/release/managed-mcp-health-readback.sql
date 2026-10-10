-- #1797: exact scoped readback без конфигурации, snapshot и credential digest.
-- Authority берётся из предоставленного владельцем PostgreSQL transport;
-- скрипт не назначает роль, не выдаёт grant и не обходит RLS.
BEGIN TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout = '10s';
SET LOCAL lock_timeout = '1s';
WITH RECURSIVE
observed AS MATERIALIZED (SELECT clock_timestamp() AS at),
scope AS MATERIALIZED (
    SELECT c.*, a.ref AS agent_ref, a.project_id AS agent_project_id,
           a.enabled AS agent_enabled, a.state AS agent_state
    FROM control_plane.integration_connections c
    JOIN control_plane.agents a ON a.organization_id=c.organization_id AND a.ref=:'agent_ref'
    WHERE c.ref=:'connection_ref' AND c.definition_key='context7'
),
grants AS MATERIALIZED (
    SELECT g.*, gc.ref AS connection_ref
    FROM scope c JOIN control_plane.integration_grants g
      ON g.organization_id=c.organization_id AND g.target_kind='AGENT'
      AND g.target_ref=c.agent_ref AND g.enabled
    JOIN control_plane.integration_connections gc ON gc.id=g.connection_id
      AND gc.organization_id=g.organization_id AND gc.definition_key='context7'
),
grant_summary AS (
    SELECT count(*) AS required,
           count(*) FILTER (WHERE g.connection_id=c.id
             AND g.risk='READ' AND g.approval_policy='NONE'
             AND g.definition_version=c.definition_version AND g.definition_digest=c.definition_digest
             AND ((g.capability_key='context7.library.resolve' AND g.ref=:'resolve_ref' AND g.version=:'resolve_version'::bigint)
               OR (g.capability_key='context7.docs.query' AND g.ref=:'query_ref' AND g.version=:'query_version'::bigint))) AS matched
    FROM grants g CROSS JOIN scope c
),
receipts AS MATERIALIZED (
    SELECT t.ref, t.purpose, t.state, t.attempt, t.predecessor_ref,
           t.created_at, t.completed_at, t.lease_expires_at,
           t.generation>0 AND t.claimed_workload='integration-gateway' AS worker_valid,
           t.lease_ref IS NOT NULL AND t.fence_digest IS NOT NULL AND t.workload_instance IS NOT NULL AS lease_bound,
           t.input_snapshot->>'connectionRef'=c.ref AS connection_ref_matches,
           t.input_snapshot->>'connectionVersion'=c.version::text AS connection_version_matches,
           t.input_snapshot->'configuration'=c.public_configuration AS configuration_matches,
           t.input_snapshot->>'definitionKey'=c.definition_key
             AND t.input_snapshot->>'definitionVersion'=c.definition_version
             AND t.input_snapshot->>'definitionDigest'=c.definition_digest AS definition_matches,
           t.input_snapshot->>'credentialRevisionRef'=cr.ref
             AND t.input_snapshot->>'credentialRevision'=cr.revision::text
             AND t.input_snapshot->>'credentialSHA256'=cr.content_sha256 AS credential_matches
    FROM scope c JOIN control_plane.integration_connection_tests t
      ON t.organization_id=c.organization_id AND t.connection_id=c.id
    LEFT JOIN control_plane.integration_credential_revisions cr
      ON cr.organization_id=c.organization_id AND cr.id=c.credential_revision_id
    ORDER BY t.created_at DESC,t.ref LIMIT 16
),
fresh AS (
    SELECT EXISTS (
      SELECT 1 FROM scope c JOIN control_plane.integration_connection_tests t
        ON t.organization_id=c.organization_id AND t.connection_id=c.id
      JOIN control_plane.integration_credential_revisions cr
        ON cr.organization_id=c.organization_id AND cr.id=c.credential_revision_id
      CROSS JOIN observed o
      WHERE t.state='SUCCEEDED' AND t.generation>0 AND t.claimed_workload='integration-gateway'
        AND t.completed_at BETWEEN o.at-INTERVAL '5 minutes' AND o.at
        AND t.input_snapshot->>'connectionRef'=c.ref
        AND t.input_snapshot->'configuration'=c.public_configuration
        AND t.input_snapshot->>'definitionKey'=c.definition_key
        AND t.input_snapshot->>'definitionVersion'=c.definition_version
        AND t.input_snapshot->>'definitionDigest'=c.definition_digest
        AND t.input_snapshot->>'credentialRevisionRef'=cr.ref
        AND t.input_snapshot->>'credentialRevision'=cr.revision::text
        AND t.input_snapshot->>'credentialSHA256'=cr.content_sha256
    ) AS valid
),
pending_roots AS MATERIALIZED (
    SELECT r.* FROM receipts r CROSS JOIN observed o
    WHERE r.purpose='MANAGED_MCP_REFRESH' AND r.state IN ('DUE','CLAIMED')
      AND r.connection_version_matches AND r.configuration_matches AND r.definition_matches AND r.credential_matches
      AND (r.state='DUE' OR (r.worker_valid AND r.lease_bound AND r.lease_expires_at>o.at))
),
attempts AS (
    SELECT r.ref,r.predecessor_ref,r.attempt,r.created_at,r.ref AS root_ref,1 AS depth
    FROM pending_roots r
    UNION ALL
    SELECT t.ref,t.predecessor_ref,t.attempt,t.created_at,a.root_ref,a.depth+1
    FROM attempts a CROSS JOIN scope c
    JOIN control_plane.integration_connection_tests t
      ON t.organization_id=c.organization_id AND t.connection_id=c.id
      AND t.ref=a.predecessor_ref AND t.purpose='MANAGED_MCP_REFRESH' AND t.attempt=a.attempt-1
    WHERE a.attempt>1 AND a.depth<3
),
pending AS (
    SELECT root_ref,count(*)=max(attempt) AS chain_complete,
           min(created_at) AS chain_created_at
    FROM attempts GROUP BY root_ref
)
SELECT jsonb_build_object(
    'observedAt',o.at,'connectionRef',:'connection_ref','agentRef',:'agent_ref',
    'scopeFound',EXISTS(SELECT 1 FROM scope),
    'connectionVersion',(SELECT version FROM scope),
    'expectedConnectionVersion',:'connection_version'::bigint,
    'connectionVersionMatches',COALESCE((SELECT version=:'connection_version'::bigint FROM scope),false),
    'connectionEnabled',COALESCE((SELECT enabled FROM scope),false),
    'connectionState',(SELECT state FROM scope),
    'connectionActive',COALESCE((SELECT lifecycle_state='ACTIVE' FROM scope),false),
    'agentEnabled',COALESCE((SELECT agent_enabled AND agent_state<>'ARCHIVED' FROM scope),false),
    'definitionVersion',(SELECT definition_version FROM scope),
    'definitionReady',COALESCE((SELECT d.enabled AND d.adapter_owner='integration-gateway'
      AND d.execution_route='MANAGED_MCP' AND d.adapter_readiness='READY'
      AND d.definition_version=c.definition_version AND d.digest=c.definition_digest
      FROM scope c JOIN control_plane.integration_definitions d ON d.stable_key=c.definition_key),false),
    'credentialConfigured',COALESCE((SELECT masked_credentials_state='CONFIGURED' AND credential_revision_id IS NOT NULL FROM scope),false),
    'requiredGrantCount',(SELECT required FROM grant_summary),
    'matchedGrantCount',(SELECT matched FROM grant_summary),
    'freshHealthReceipt',(SELECT valid FROM fresh),
    'refreshEligible',COALESCE((SELECT control_plane.context7_health_refresh_eligible(organization_id,id) FROM scope),false),
    'pendingChainComplete',EXISTS(SELECT 1 FROM pending WHERE chain_complete),
    'pendingWithinWindow',EXISTS(SELECT 1 FROM pending WHERE chain_complete AND chain_created_at BETWEEN o.at-INTERVAL '30 seconds' AND o.at),
    'receipts',COALESCE((SELECT jsonb_agg(jsonb_build_object(
      'ref',ref,'purpose',purpose,'state',state,'attempt',attempt,'createdAt',created_at,'completedAt',completed_at,
      'workerValid',COALESCE(worker_valid,false),'connectionRefMatches',COALESCE(connection_ref_matches,false),
      'connectionVersionMatches',COALESCE(connection_version_matches,false),'configurationMatches',COALESCE(configuration_matches,false),
      'definitionMatches',COALESCE(definition_matches,false),'credentialMatches',COALESCE(credential_matches,false)
    ) ORDER BY created_at DESC,ref) FROM receipts),'[]'::jsonb)
)::text FROM observed o;
COMMIT;
