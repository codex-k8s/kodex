BEGIN READ ONLY;
SET LOCAL statement_timeout = '5s';
SET LOCAL lock_timeout = '1s';
SET LOCAL idle_in_transaction_session_timeout = '5s';
WITH operation AS (
    SELECT * FROM internal_rpc_authority.authority_rotation_operations
    ORDER BY registry_revision DESC LIMIT 1
), phase AS (
    SELECT p.* FROM internal_rpc_authority.authority_rotation_operation_phase_intents p
    JOIN operation o ON o.operation_id = p.operation_id WHERE p.phase = 'DISTRIBUTE'
), intent AS (
    SELECT i.* FROM internal_rpc_authority.authority_rotation_intents i
    JOIN phase p ON p.publication_intent_id = i.intent_id
), history AS (
    SELECT h.* FROM internal_rpc_authority.authority_snapshot_history h
    JOIN phase p ON p.source_revision = h.source_revision
      AND p.publication_intent_id = h.publication_intent_id
), publication AS (
    SELECT p.* FROM internal_rpc_authority.authority_rotation_operation_publications p
    JOIN operation o ON o.operation_id = p.operation_id
), function_metadata AS (
    SELECT jsonb_build_object(
        'oid', p.oid::text, 'owner', pg_get_userbyid(p.proowner),
        'acl', p.proacl::text, 'securityDefiner', p.prosecdef,
        'volatility', p.provolatile, 'config', p.proconfig,
        'result', pg_get_function_result(p.oid),
        'args', pg_get_function_identity_arguments(p.oid),
        'argNames', p.proargnames, 'language', l.lanname,
        'parallel', p.proparallel, 'strict', p.proisstrict,
        'leakproof', p.proleakproof, 'returnsSet', p.proretset,
        'cost', p.procost, 'rows', p.prorows,
        'definitionSHA256', encode(sha256(convert_to(pg_get_functiondef(p.oid), 'UTF8')), 'hex')
    ) AS value FROM pg_proc p
    JOIN pg_namespace n ON n.oid = p.pronamespace
    JOIN pg_language l ON l.oid = p.prolang
    WHERE n.nspname = 'internal_rpc_authority'
      AND p.proname = 'authority_rotation_status' AND p.pronargs = 0
), ledger AS (
    SELECT jsonb_build_object(
        'operation', (SELECT to_jsonb(o) FROM operation o),
        'phase', (SELECT to_jsonb(p) FROM phase p),
        'intent', (SELECT to_jsonb(i) FROM intent i),
        'publications', (SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY p.source_revision), '[]') FROM publication p),
        'historyHashes', (SELECT coalesce(jsonb_agg(
            encode(sha256(convert_to(to_jsonb(h)::text, 'UTF8')), 'hex') ORDER BY h.source_revision), '[]') FROM history h)
    ) AS value
)
SELECT jsonb_build_object(
    'observedAt', clock_timestamp(),
    'gooseVersion', (SELECT max(version_id) FROM public.goose_db_version WHERE is_applied),
    'gooseRows', (SELECT count(*) FROM public.goose_db_version),
    'goosePredecessorSHA256', (SELECT encode(sha256(convert_to(jsonb_agg(to_jsonb(g) ORDER BY g.id)::text, 'UTF8')), 'hex')
        FROM public.goose_db_version g WHERE g.version_id <= 20260912000200),
    'function', (SELECT value FROM function_metadata),
    'owner', internal_rpc_authority.authority_rotation_status(),
    'operation', (SELECT jsonb_build_object('id', o.operation_id, 'status', o.status,
        'registryRevision', o.registry_revision, 'registryDigest', o.registry_digest_sha256,
        'baseRevision', o.base_revision, 'baseDigest', o.base_digest_sha256,
        'expectedReaders', o.expected_readback_count) FROM operation o),
    'phase', (SELECT jsonb_build_object('phase', p.phase, 'intentID', p.publication_intent_id,
        'sourceRevision', p.source_revision, 'registryDigest', p.registry_digest_sha256,
        'inputDigest', p.publication_input_digest_sha256) FROM phase p),
    'intent', (SELECT jsonb_build_object('id', i.intent_id, 'status', i.status,
        'protocol', i.protocol_version, 'sourceRevision', i.source_revision,
        'sourceDigest', i.source_digest_sha256, 'predecessorRevision', i.predecessor_revision,
        'predecessorDigest', i.predecessor_digest_sha256, 'expectedReaders', i.expected_readback_count,
        'deliveredAt', i.delivered_at, 'promotedAt', i.promoted_at, 'retiredAt', i.retired_at,
        'abortedAt', i.aborted_at) FROM intent i),
    'snapshot', (SELECT jsonb_build_object('sourceRevision', h.source_revision,
        'sourceDigest', h.source_digest_sha256, 'policyRevision', h.policy_revision,
        'intentID', h.publication_intent_id, 'inputDigest', h.publication_input_digest_sha256,
        'predecessorRevision', h.predecessor_revision, 'predecessorDigest', h.predecessor_digest_sha256,
        'expectedReaders', h.expected_readback_count, 'publishedAt', h.published_at) FROM history h),
    'completedPhasePublicationCount', (SELECT count(*) FROM publication),
    'ledgerSHA256', (SELECT encode(sha256(convert_to(value::text, 'UTF8')), 'hex') FROM ledger)
);
ROLLBACK;
