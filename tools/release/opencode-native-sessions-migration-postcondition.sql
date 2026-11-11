BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout='5s';
SET LOCAL lock_timeout='500ms';
SET LOCAL search_path=pg_catalog;
SELECT jsonb_build_object(
 'profile','OPENCODE_NATIVE_V1',
 'database',current_database(),'readOnly',current_setting('transaction_read_only'),
 'gooseVersion',(SELECT max(version_id) FROM public.goose_db_version WHERE is_applied),
 'nativeColumnsReady',(SELECT count(*)=2 FROM pg_attribute WHERE attrelid IN(
   'control_plane.session_storage'::regclass,'control_plane.session_archives'::regclass)
   AND attname='native_checkpoint' AND atttypid='jsonb'::regtype AND NOT attisdropped),
 'legacyIdentityAbsent',NOT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid IN(
   'control_plane.session_storage'::regclass,'control_plane.session_archives'::regclass)
   AND attname='codex_session_id' AND NOT attisdropped),
 'nativeShapeReady',EXISTS(SELECT 1 FROM pg_proc WHERE oid=to_regprocedure('control_plane.native_checkpoint_shape(jsonb)')
   AND md5(prosrc)='f8e63f66c2c84d91b1578ffd2a9a2cfb'
   AND proowner='control_plane_owner'::regrole AND NOT prosecdef AND provolatile='i'
   AND proconfig=ARRAY['search_path=pg_catalog'] AND prorettype='boolean'::regtype
   AND has_function_privilege('control_plane_runtime',oid,'EXECUTE')
   AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(proacl,acldefault('f',proowner))) acl WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE')),
 'nativeIdentityGuardReady',EXISTS(SELECT 1 FROM pg_trigger t JOIN pg_proc p ON p.oid=t.tgfoid
   WHERE t.tgrelid='control_plane.session_storage'::regclass AND t.tgname='guard_native_session_identity'
   AND p.oid=to_regprocedure('control_plane.guard_native_session_identity()') AND md5(p.prosrc)='709ce166aefcebeec53b34d350783ab0'
   AND NOT t.tgisinternal AND t.tgenabled='O' AND p.proowner='control_plane_owner'::regrole AND NOT p.prosecdef
   AND p.proconfig=ARRAY['search_path=pg_catalog']),
 'nativeConstraintsReady',(SELECT count(*)=4 FROM pg_constraint WHERE conrelid IN(
   'control_plane.session_storage'::regclass,'control_plane.session_archives'::regclass)
   AND conname IN('session_storage_native_shape','session_storage_native_writer','session_storage_native_writer_state','session_archives_format_shape')
   AND contype='c' AND convalidated AND conenforced
   AND md5(pg_get_constraintdef(oid))=CASE conname
     WHEN 'session_storage_native_shape' THEN '94484ba78c04cdc27c965a762251d5fa'
     WHEN 'session_storage_native_writer' THEN '646650da389a94086484be985fd2afa8'
     WHEN 'session_storage_native_writer_state' THEN 'aa4b4c485e1b52c698b77625c47f3d97'
     WHEN 'session_archives_format_shape' THEN 'd64342cfa30ee743cb6e72a729d519ae' END)
)::text;
ROLLBACK;
