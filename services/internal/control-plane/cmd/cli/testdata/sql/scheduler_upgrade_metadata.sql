-- name: scheduler_upgrade_metadata :one
SELECT
    current_user = session_user AND session_user = 'control_plane_migrator'
    AND (SELECT count(*) FROM public.goose_db_version) = 1
    AND EXISTS (
        SELECT 1 FROM public.goose_db_version WHERE version_id = 0 AND is_applied
    )
    AND pg_get_userbyid((
        SELECT relowner FROM pg_class WHERE oid = 'public.goose_db_version'::regclass
    )) = 'control_plane_owner'
    AND pg_get_userbyid((
        SELECT relowner FROM pg_class WHERE oid = 'public.goose_db_version_id_seq'::regclass
    )) = 'control_plane_owner'
    AND has_table_privilege(session_user, 'public.goose_db_version', 'SELECT')
    AND has_table_privilege(session_user, 'public.goose_db_version', 'INSERT')
    AND has_table_privilege(session_user, 'public.goose_db_version', 'UPDATE')
    AND has_table_privilege(session_user, 'public.goose_db_version', 'DELETE')
    AND has_sequence_privilege(session_user, 'public.goose_db_version_id_seq', 'USAGE')
    AND has_sequence_privilege(session_user, 'public.goose_db_version_id_seq', 'SELECT')
    AND has_sequence_privilege(session_user, 'public.goose_db_version_id_seq', 'UPDATE')
    AND NOT EXISTS (
        SELECT 1 FROM pg_roles
        WHERE rolname = session_user
            AND (rolinherit OR rolsuper OR rolcreaterole OR rolcreatedb OR rolbypassrls)
    );
