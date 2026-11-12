BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout='5s';
SET LOCAL lock_timeout='500ms';
SET LOCAL search_path=pg_catalog;
WITH RECURSIVE nodes(id) AS (
 SELECT 'control_plane.projects'::regclass::oid UNION
 SELECT c.conrelid FROM pg_constraint c JOIN nodes n ON c.confrelid=n.id
 WHERE c.contype='f' AND c.connamespace='control_plane'::regnamespace
), graph AS (
 SELECT count(*) AS edges,md5(string_agg(cn.nspname||'.'||cc.relname||'|'||c.conname||'|'||pn.nspname||'.'||pc.relname||'|'||c.confdeltype::text,
  E'\n' ORDER BY cn.nspname,cc.relname,c.conname)) AS digest
 FROM pg_constraint c JOIN pg_class cc ON cc.oid=c.conrelid JOIN pg_namespace cn ON cn.oid=cc.relnamespace
 JOIN pg_class pc ON pc.oid=c.confrelid JOIN pg_namespace pn ON pn.oid=pc.relnamespace
 WHERE c.contype='f' AND c.conrelid IN(SELECT id FROM nodes) AND c.confrelid IN(SELECT id FROM nodes)
)
SELECT jsonb_build_object(
 'profile','OPENCODE_NATIVE_V1',
 'database',current_database(),'readOnly',current_setting('transaction_read_only'),
 'gooseVersion',(SELECT max(version_id) FROM public.goose_db_version WHERE is_applied),
 'nativeWriterForeignKeysReady',(SELECT count(*)=2 AND bool_and(c.contype='f' AND c.convalidated AND c.conenforced
   AND c.condeferrable AND NOT c.condeferred AND pg_get_constraintdef(c.oid)=CASE c.conname
    WHEN 'session_storage_native_writer_lease_ref_fkey' THEN 'FOREIGN KEY (native_writer_lease_ref) REFERENCES control_plane.runtime_leases(ref) DEFERRABLE'
    WHEN 'session_storage_native_writer_revision_id_fkey' THEN 'FOREIGN KEY (native_writer_revision_id) REFERENCES control_plane.runtime_revisions(id) DEFERRABLE' END)
   FROM pg_constraint c WHERE c.conrelid='control_plane.session_storage'::regclass
   AND c.conname IN('session_storage_native_writer_lease_ref_fkey','session_storage_native_writer_revision_id_fkey')),
 'nativePurgeGraphReady',(SELECT edges=308 AND digest='d6aa5b9aa21c5e065c3106f2a6119e0f' FROM graph)
   AND (SELECT count(*)=111 AND md5(string_agg(n.nspname||'.'||r.relname,E'\n' ORDER BY n.nspname,r.relname))='3eb558fae167872ba6bd17bc68f7a303'
    FROM nodes JOIN pg_class r ON r.oid=nodes.id JOIN pg_namespace n ON n.oid=r.relnamespace),
 'nativePurgeGuardReady',EXISTS(SELECT 1 FROM pg_proc p
   WHERE p.oid='control_plane.purge_project_database(uuid,uuid,text)'::regprocedure AND p.proowner='control_plane_owner'::regrole
   AND p.prosecdef AND p.provolatile='v' AND p.prorettype='integer'::regtype AND NOT p.proretset
   AND p.proconfig=ARRAY['search_path=pg_catalog']
   AND md5(replace(replace(p.prosrc,
    'IF v_nodes <> 111 OR v_node_fingerprint <> ''3eb558fae167872ba6bd17bc68f7a303'' THEN',
    'IF v_nodes <> 105 OR v_node_fingerprint <> ''f51a0de25bd942a250997775617eeaee'' THEN'),
    'IF v_edges <> 308 OR v_edge_fingerprint <> ''d6aa5b9aa21c5e065c3106f2a6119e0f'' THEN',
    'IF v_edges <> 284 OR v_edge_fingerprint <> ''6bf6ad76d8c7ae66e07f75ecf719fdd6'' THEN'))='b886c24d4e2f5c4a0ec08123d99462b0'
   AND has_function_privilege('control_plane_runtime',p.oid,'EXECUTE')
   AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl
    WHERE acl.grantee NOT IN('control_plane_owner'::regrole,'control_plane_runtime'::regrole)
    OR acl.grantee='control_plane_runtime'::regrole AND (acl.is_grantable OR acl.privilege_type<>'EXECUTE'))),
 'runtimeFileVisibilityReady',EXISTS(SELECT 1 FROM pg_proc p
   WHERE p.oid='control_plane.runtime_file_source_visible(uuid,uuid,uuid,uuid,uuid,text,text,uuid)'::regprocedure
   AND md5(p.prosrc)='6e6494d533d00f1bcd35d65497dff28c' AND p.proowner='control_plane_owner'::regrole
   AND NOT p.prosecdef AND p.provolatile='s' AND p.prorettype='boolean'::regtype AND NOT p.proretset
   AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='sql')
   AND p.proconfig=ARRAY['search_path=pg_catalog, control_plane']
   AND has_function_privilege('control_plane_runtime',p.oid,'EXECUTE')
   AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl
     WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE')),
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
