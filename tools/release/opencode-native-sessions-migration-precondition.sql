BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout='5s';
SET LOCAL lock_timeout='500ms';
SET LOCAL search_path=pg_catalog;
SELECT jsonb_build_object(
 'profile','OPENCODE_NATIVE_PREDECESSOR_V1',
 'database',current_database(),'readOnly',current_setting('transaction_read_only'),
 'gooseVersion',(SELECT max(version_id) FROM public.goose_db_version WHERE is_applied),
 'sessionDataEmpty',NOT EXISTS(SELECT 1 FROM control_plane.sessions)
   AND NOT EXISTS(SELECT 1 FROM control_plane.session_storage)
   AND NOT EXISTS(SELECT 1 FROM control_plane.session_archives),
 'nativeColumnsAbsent',NOT EXISTS(SELECT 1 FROM pg_attribute
   WHERE attrelid='control_plane.session_storage'::regclass AND attname='native_checkpoint' AND NOT attisdropped),
 'activeRuntimeLeases',(SELECT count(*) FROM control_plane.runtime_leases WHERE state='CLAIMED' AND expires_at>clock_timestamp())
)::text;
ROLLBACK;
