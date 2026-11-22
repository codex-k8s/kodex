-- name: workflow_file_provenance__authority :one
SELECT pg_catalog.current_database() = 'control_plane'
    AND (SELECT count(*) = 1 FROM public.goose_db_version WHERE is_applied AND version_id = 20261121000100)
    AND (SELECT count(*) = 1 FROM public.goose_db_version WHERE is_applied AND version_id = $2)
    AND p.proowner = 'control_plane_owner'::regrole
    AND NOT p.prosecdef
    AND pg_catalog.md5(p.prosrc) = $1
    AND pg_catalog.has_table_privilege('kodex_backup_reader', storage.oid, 'SELECT')
    AND NOT pg_catalog.has_table_privilege('kodex_backup_reader', storage.oid, 'INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER,MAINTAIN')
    AND NOT pg_catalog.has_function_privilege('kodex_backup_reader', p.oid, 'EXECUTE')
FROM pg_catalog.pg_namespace n
JOIN pg_catalog.pg_proc p ON p.pronamespace = n.oid
JOIN pg_catalog.pg_class storage ON storage.relnamespace = n.oid AND storage.relname = 'session_workspace_storage'
WHERE storage.relkind IN ('r', 'p')
    AND storage.relowner = 'control_plane_owner'::regrole
    AND n.nspname = 'control_plane'
    AND p.proname = 'runtime_file_source_visible_revision'
    AND pg_catalog.oidvectortypes(p.proargtypes) = 'uuid, uuid, uuid, uuid, uuid, text, text, uuid, uuid';
