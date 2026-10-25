-- name: repository_bundle_migration_postcondition :one
-- Только schema/authority metadata; source bytes, credentials и бизнес-payload исключены.
BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout = '5s';
SET LOCAL lock_timeout = '500ms';
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
    'purgeGraphGuard',position('IF v_nodes <> 105 OR v_node_fingerprint <>' in pg_get_functiondef('control_plane.purge_project_database(uuid,uuid,text)'::regprocedure))>0
        AND position('IF v_edges <> 289 OR v_edge_fingerprint <>' in pg_get_functiondef('control_plane.purge_project_database(uuid,uuid,text)'::regprocedure))>0
)::text;
ROLLBACK;
