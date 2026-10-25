-- name: repository_bundle_migration_postcondition :one
-- Только schema/authority metadata; source bytes, credentials и бизнес-payload исключены.
BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout = '5s';
SET LOCAL lock_timeout = '500ms';
-- Граф удаления включает только project-descendants: из 13 FK новой таблицы
-- семь ведут внутрь графа, остальные ведут к общим organization/subject/connection.
-- Сверяем и фактический каталог, и закреплённый guard; число всех FK их не заменяет.
WITH RECURSIVE nodes(relation_id) AS (
    SELECT 'control_plane.projects'::regclass::oid UNION
    SELECT c.conrelid FROM nodes n JOIN pg_constraint c ON c.confrelid=n.relation_id
        AND c.contype='f' AND c.connamespace='control_plane'::regnamespace
), node_proof AS (
    SELECT count(*) AS count, md5(string_agg(n.nspname||'.'||c.relname,E'\n' ORDER BY n.nspname,c.relname)) AS digest
    FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE c.oid IN (SELECT relation_id FROM nodes)
), edge_proof AS (
    SELECT count(*) AS count,
        md5(string_agg(cn.nspname||'.'||cc.relname||'|'||c.conname||'|'||pn.nspname||'.'||pc.relname||'|'||c.confdeltype::text,
            E'\n' ORDER BY cn.nspname,cc.relname,c.conname)) AS digest
    FROM pg_constraint c JOIN pg_class cc ON cc.oid=c.conrelid JOIN pg_namespace cn ON cn.oid=cc.relnamespace
    JOIN pg_class pc ON pc.oid=c.confrelid JOIN pg_namespace pn ON pn.oid=pc.relnamespace
    WHERE c.contype='f' AND c.confrelid IN (SELECT relation_id FROM nodes) AND c.conrelid IN (SELECT relation_id FROM nodes)
), guards AS (
    SELECT pg_get_functiondef('control_plane.purge_project_database(uuid,uuid,text)'::regprocedure) AS definition
)
SELECT jsonb_build_object(
    'readOnly',current_setting('transaction_read_only'),
    'database',current_database(),
    'gooseVersion',(SELECT max(version_id) FILTER (WHERE is_applied) FROM public.goose_db_version),
    'tableOwner',(SELECT pg_get_userbyid(relowner) FROM pg_class WHERE oid='control_plane.repository_bundle_publications'::regclass),
    'noBodyColumns',NOT EXISTS (SELECT 1 FROM pg_attribute
        WHERE attrelid='control_plane.repository_bundle_publications'::regclass
          AND NOT attisdropped AND attnum>0 AND (atttypid='bytea'::regtype OR attname IN ('archive_bytes','manifest_bytes'))),
    'foreignKeys',(SELECT count(*) FROM pg_constraint WHERE conrelid='control_plane.repository_bundle_publications'::regclass AND contype='f'),
    'deferredForeignKeys',(SELECT count(*) FROM pg_constraint WHERE conrelid='control_plane.repository_bundle_publications'::regclass AND contype='f' AND condeferrable),
    'guardEnabled',EXISTS (SELECT 1 FROM pg_trigger WHERE tgrelid='control_plane.repository_bundle_publications'::regclass
        AND tgname='repository_bundle_publication_guard' AND tgenabled='O' AND NOT tgisinternal),
    'retentionConsumer',position('control_plane.repository_bundle_publications' in pg_get_functiondef('control_plane.artifact_has_retained_revisions(uuid)'::regprocedure))>0
        AND position('UNKNOWN_OUTCOME' in pg_get_functiondef('control_plane.artifact_has_retained_revisions(uuid)'::regprocedure))>0,
    'purgeNodes',(SELECT count FROM node_proof),
    'purgeEdges',(SELECT count FROM edge_proof),
    'purgeNodeFingerprint',(SELECT digest FROM node_proof),
    'purgeEdgeFingerprint',(SELECT digest FROM edge_proof),
    'purgeGraphGuard',(SELECT count=105 AND digest='f51a0de25bd942a250997775617eeaee' FROM node_proof)
        AND (SELECT count=283 AND digest='be6e23d9e894687a69a19e9b5777ab09' FROM edge_proof)
        AND (SELECT regexp_count(definition,'IF v_nodes <> [0-9]+ OR v_node_fingerprint <> ''[a-f0-9]{32}'' THEN')=1
            AND regexp_count(definition,'IF v_edges <> [0-9]+ OR v_edge_fingerprint <> ''[a-f0-9]{32}'' THEN')=1
            AND position('IF v_nodes <> 105 OR v_node_fingerprint <> ''f51a0de25bd942a250997775617eeaee'' THEN' in definition)>0
            AND position('IF v_edges <> 283 OR v_edge_fingerprint <> ''be6e23d9e894687a69a19e9b5777ab09'' THEN' in definition)>0
            FROM guards)
)::text;
ROLLBACK;
