-- Точный017 каталог, включая parent-side FK и origin-close triggers; business rows не читаются.
DO $volume_catalog$
DECLARE v_actual jsonb; v_path text;
BEGIN
 v_path:=current_setting('search_path'); PERFORM set_config('search_path','pg_catalog',true);
-- Только точные schema metadata трёх custody-таблиц и шести функций; business rows не читаются.
WITH targets AS (
 SELECT c.* FROM pg_catalog.pg_class c WHERE c.oid IN (
 'control_plane.session_workspace_volumes'::regclass,
 'control_plane.session_workspace_volume_tasks'::regclass,
 'control_plane.session_workspace_volume_attempts'::regclass)
), functions AS (
 SELECT p.* FROM pg_catalog.pg_proc p WHERE p.oid IN (
 'control_plane.guard_workspace_volume_custody()'::regprocedure,
 'control_plane.guard_workspace_volume_task()'::regprocedure,
 'control_plane.guard_workspace_volume_attempt()'::regprocedure,
 'control_plane.guard_workspace_volume_envelope()'::regprocedure,
 'control_plane.guard_workspace_volume_lineage()'::regprocedure,
 'control_plane.close_workspace_volume_origin()'::regprocedure)
), catalogs AS (
 SELECT r.relname,pg_catalog.jsonb_build_object(
 'relation',pg_catalog.jsonb_build_array(pg_catalog.pg_get_userbyid(r.relowner),r.relkind,r.relpersistence,
 r.relrowsecurity,r.relforcerowsecurity,r.relispartition,
 EXISTS(SELECT 1 FROM pg_catalog.pg_inherits WHERE inhrelid=r.oid OR inhparent=r.oid)),
 'columns',(SELECT pg_catalog.jsonb_agg(pg_catalog.jsonb_build_array(a.attname,
 pg_catalog.format_type(a.atttypid,a.atttypmod),a.attnotnull,pg_catalog.pg_get_expr(d.adbin,d.adrelid),
 a.attidentity,a.attgenerated,a.attacl::text) ORDER BY a.attnum)
 FROM pg_catalog.pg_attribute a LEFT JOIN pg_catalog.pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum
 WHERE a.attrelid=r.oid AND a.attnum>0 AND NOT a.attisdropped),
 'constraints',(SELECT pg_catalog.jsonb_agg(pg_catalog.jsonb_build_array(c.conname,c.contype,c.convalidated,
 c.conenforced,c.condeferrable,c.condeferred,pg_catalog.pg_get_constraintdef(c.oid)) ORDER BY c.conname)
 FROM pg_catalog.pg_constraint c WHERE c.conrelid=r.oid),
 'indexes',(SELECT pg_catalog.jsonb_agg(pg_catalog.jsonb_build_array(c.relname,i.indisunique,i.indisvalid,
 i.indisready,i.indislive,pg_catalog.pg_get_indexdef(i.indexrelid)) ORDER BY c.relname)
 FROM pg_catalog.pg_index i JOIN pg_catalog.pg_class c ON c.oid=i.indexrelid WHERE i.indrelid=r.oid),
 'policies',(SELECT pg_catalog.jsonb_agg(pg_catalog.jsonb_build_array(p.polname,p.polcmd,p.polpermissive,
 (SELECT pg_catalog.jsonb_agg(pg_catalog.pg_get_userbyid(role) ORDER BY role) FROM pg_catalog.unnest(p.polroles) role),
 pg_catalog.pg_get_expr(p.polqual,p.polrelid),pg_catalog.pg_get_expr(p.polwithcheck,p.polrelid)) ORDER BY p.polname)
 FROM pg_catalog.pg_policy p WHERE p.polrelid=r.oid),
 'acl',(SELECT pg_catalog.jsonb_agg(pg_catalog.jsonb_build_array(pg_catalog.pg_get_userbyid(a.grantee),
 a.privilege_type,a.is_grantable) ORDER BY pg_catalog.pg_get_userbyid(a.grantee),a.privilege_type)
 FROM pg_catalog.aclexplode(coalesce(r.relacl,pg_catalog.acldefault('r',r.relowner))) a)
 ) AS metadata FROM targets r
), function_catalog AS (
 SELECT p.oid::regprocedure::text AS signature,pg_catalog.jsonb_build_array(pg_catalog.md5(p.prosrc),
 pg_catalog.pg_get_userbyid(p.proowner),p.prosecdef,p.provolatile,p.proconfig,p.prorettype::regtype::text,
 l.lanname,p.proretset,p.prokind,p.proparallel,p.proleakproof,
 (SELECT pg_catalog.jsonb_agg(pg_catalog.jsonb_build_array(pg_catalog.pg_get_userbyid(a.grantee),
 a.privilege_type,a.is_grantable) ORDER BY pg_catalog.pg_get_userbyid(a.grantee),a.privilege_type)
 FROM pg_catalog.aclexplode(coalesce(p.proacl,pg_catalog.acldefault('f',p.proowner))) a)) AS metadata
 FROM functions p JOIN pg_catalog.pg_language l ON l.oid=p.prolang
), trigger_catalog AS (
 SELECT r.oid::regclass::text AS relation,
 CASE WHEN t.tgisinternal THEN pg_catalog.regexp_replace(t.tgname,'_[0-9]+$','_<oid>') ELSE t.tgname END AS name,
 pg_catalog.jsonb_build_array(t.tgtype,t.tgenabled,t.tgisinternal,t.tgdeferrable,t.tginitdeferred,
 t.tgnargs,t.tgattr::text,
 pg_catalog.replace(pg_catalog.pg_get_triggerdef(t.oid),pg_catalog.quote_ident(t.tgname),
 pg_catalog.quote_ident(CASE WHEN t.tgisinternal THEN pg_catalog.regexp_replace(t.tgname,'_[0-9]+$','_<oid>') ELSE t.tgname END)),
 t.tgfoid::regprocedure::text,
 c.conname,c.conrelid::regclass::text,c.confrelid::regclass::text,t.tgparentid=0,
 pg_catalog.encode(t.tgargs,'hex'),t.tgoldtable,t.tgnewtable) AS metadata
 FROM pg_catalog.pg_trigger t JOIN pg_catalog.pg_class r ON r.oid=t.tgrelid
 LEFT JOIN pg_catalog.pg_constraint c ON c.oid=t.tgconstraint
 WHERE t.tgrelid IN(SELECT oid FROM targets) OR t.tgfoid IN(SELECT oid FROM functions)
 OR t.tgconstraint IN(SELECT oid FROM pg_catalog.pg_constraint WHERE conrelid IN(SELECT oid FROM targets))
)
SELECT pg_catalog.jsonb_build_object(
 'tables',(SELECT pg_catalog.jsonb_object_agg(relname,
 (SELECT pg_catalog.jsonb_object_agg(key,pg_catalog.md5(value::text)) FROM pg_catalog.jsonb_each(metadata))) FROM catalogs),
 'functions',(SELECT pg_catalog.jsonb_object_agg(signature,pg_catalog.md5(metadata::text)) FROM function_catalog),
 'triggers',(SELECT pg_catalog.md5(pg_catalog.jsonb_agg(pg_catalog.jsonb_build_array(relation,name,metadata)
 ORDER BY relation,name,metadata::text)::text) FROM trigger_catalog)
) INTO v_actual;
 PERFORM set_config('search_path',v_path,true);
 IF v_actual IS DISTINCT FROM '{"functions":{"control_plane.close_workspace_volume_origin()":"5291b3aed4b5e74f32594ec28323fd35","control_plane.guard_workspace_volume_attempt()":"af2656c96fcaf66518e162266b0cd8ae","control_plane.guard_workspace_volume_custody()":"42f2f7ee1abe1d4939a16813987e86a1","control_plane.guard_workspace_volume_envelope()":"1d6c81067ae534b34906a31d2416d043","control_plane.guard_workspace_volume_lineage()":"19bca2c2c9b6e7e6aee2db7af7b2d3f8","control_plane.guard_workspace_volume_task()":"cc2b40d0e19ef732523d19ee6a255161"},"tables":{"session_workspace_volume_attempts":{"acl":"4a534d150446bd64168071defd5f0aa7","columns":"65f69d624ca71a6b26c3042e5d408601","constraints":"9ab1e77c00b1656468a0dfa3abf22c76","indexes":"a7a4712dd8091856442798a0a7f59384","policies":"37a6259cc0c1dae299a7866489dff0bd","relation":"cdefe876d5d2436e0650dc9eda73dca7"},"session_workspace_volume_tasks":{"acl":"4a534d150446bd64168071defd5f0aa7","columns":"75aedf9114fe1e29a13b197c9bfedf1c","constraints":"c6b12d1caa8cb781b16a27f4fa3cc903","indexes":"bbb50fe3cfad8b4f53295ac4537923dd","policies":"37a6259cc0c1dae299a7866489dff0bd","relation":"cdefe876d5d2436e0650dc9eda73dca7"},"session_workspace_volumes":{"acl":"4a534d150446bd64168071defd5f0aa7","columns":"8d22b38c47191a7d407ef32d74b2725d","constraints":"38c72afaed1de9b49cdbfc7e08816007","indexes":"08bb1b92b17005401d2ba80c6d824feb","policies":"37a6259cc0c1dae299a7866489dff0bd","relation":"cdefe876d5d2436e0650dc9eda73dca7"}},"triggers":"89e9e0d1a516be07cf9994c7ffdf15c5"}'::jsonb THEN
 RAISE EXCEPTION 'workspace volume catalog changed';
 END IF;
END $volume_catalog$;
