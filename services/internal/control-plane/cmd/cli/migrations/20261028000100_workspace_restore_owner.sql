-- +goose Up
-- Новый профиль расширяет только restore custody; применённые015/016/017/SSH неизменны.
SET ROLE control_plane_owner;
-- +goose StatementBegin
DO $predecessor$
BEGIN
 IF (SELECT max(version_id) FROM public.goose_db_version WHERE is_applied) IS DISTINCT FROM 20261027000100::bigint THEN
   RAISE EXCEPTION 'workspace restore requires exact volume SSH profile';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM pg_catalog.pg_proc p
   WHERE p.oid='control_plane.guard_workspace_archive_task()'::regprocedure
   AND md5(p.prosrc)='2198d3bff772de11aade05248500e0f1' AND p.proowner='control_plane_owner'::regrole
   AND NOT p.prosecdef AND p.provolatile='v' AND p.prorettype='trigger'::regtype
   AND p.proconfig=ARRAY['search_path=pg_catalog']
   AND p.prolang=(SELECT oid FROM pg_catalog.pg_language WHERE lanname='plpgsql')
   AND has_function_privilege('control_plane_runtime',p.oid,'EXECUTE')
   AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a
     WHERE a.grantee=0 AND a.privilege_type='EXECUTE')) THEN
   RAISE EXCEPTION 'workspace restore task guard preimage changed';
 END IF;
END $predecessor$;
-- +goose StatementEnd
SET LOCAL lock_timeout = '8s';
SET LOCAL statement_timeout = '30s';
-- Read-only точный каталог workspace; не читает business rows или секреты.
-- +goose StatementBegin
DO $workspace_catalog$
DECLARE v_path text; v_catalog jsonb;
 v_relation oid:=to_regclass('control_plane.session_workspace_storage');
BEGIN
 IF v_relation IS NULL OR to_regprocedure('control_plane.guard_session_workspace_storage()') IS NULL THEN
   RAISE EXCEPTION 'session workspace schema is missing';
 END IF;
   v_path:=current_setting('search_path');
   PERFORM set_config('search_path','pg_catalog',true);
SELECT jsonb_build_object(
'columns',(SELECT jsonb_agg(jsonb_build_array(a.attname,format_type(a.atttypid,a.atttypmod),a.attnotnull,pg_get_expr(d.adbin,d.adrelid),a.attidentity,a.attgenerated) ORDER BY a.attnum) FROM pg_attribute a LEFT JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum WHERE a.attrelid='control_plane.session_workspace_storage'::regclass AND a.attnum>0 AND NOT a.attisdropped),
'constraints',(SELECT jsonb_agg(jsonb_build_array(c.conname,c.contype,c.convalidated,c.conenforced,c.condeferrable,c.condeferred,pg_get_constraintdef(c.oid)) ORDER BY c.conname) FROM pg_constraint c WHERE c.conrelid='control_plane.session_workspace_storage'::regclass),
'indexes',(SELECT jsonb_agg(jsonb_build_array(c.relname,i.indisunique,i.indisvalid,i.indisready,pg_get_indexdef(i.indexrelid)) ORDER BY c.relname) FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid WHERE i.indrelid='control_plane.session_workspace_storage'::regclass),
'triggers',(SELECT jsonb_agg(jsonb_build_array(t.tgname,t.tgtype,t.tgenabled,t.tgisinternal,t.tgdeferrable,t.tginitdeferred,t.tgnargs,t.tgattr::text,pg_get_expr(t.tgqual,t.tgrelid),t.tgfoid::regprocedure::text) ORDER BY t.tgname) FROM pg_trigger t WHERE t.tgrelid='control_plane.session_workspace_storage'::regclass AND NOT t.tgisinternal),
'function',(SELECT jsonb_build_array(md5(p.prosrc),pg_get_userbyid(p.proowner),p.prosecdef,p.provolatile,p.proconfig,p.prorettype::regtype::text,l.lanname) FROM pg_proc p JOIN pg_language l ON l.oid=p.prolang WHERE p.oid='control_plane.guard_session_workspace_storage()'::regprocedure)) INTO v_catalog;
   PERFORM set_config('search_path',v_path,true);
   IF
       md5((v_catalog->'columns')::text) IS DISTINCT FROM '2504916f99e7d1a367c0cc609ee821fa' OR
       md5((v_catalog->'constraints')::text) IS DISTINCT FROM '62adb05daddf7ff16ed49e28824b482c' OR
       md5((v_catalog->'indexes')::text) IS DISTINCT FROM '4c100952cb07fc7a435afbe70a71d0ed' OR
       md5((v_catalog->'triggers')::text) IS DISTINCT FROM '270cf9cc1d1038e485ac2a741144ce8c' OR
       md5((v_catalog->'function')::text) IS DISTINCT FROM '198323a1c336ed672c874d49b1698ff2' THEN
     RAISE EXCEPTION 'session workspace schema changed';
   END IF;
   -- conenforced не доказывает, что внутренние FK triggers включены.
   IF (SELECT count(*) FROM pg_constraint WHERE conrelid=v_relation AND contype='f')<>8 OR EXISTS (
     SELECT 1 FROM pg_constraint c WHERE c.conrelid=v_relation AND c.contype='f' AND (
       (SELECT count(*) FROM pg_trigger t WHERE t.tgconstraint=c.oid)<>4 OR
       (SELECT count(DISTINCT t.tgfoid) FROM pg_trigger t WHERE t.tgconstraint=c.oid)<>4 OR EXISTS (
         SELECT 1 FROM pg_trigger t WHERE t.tgconstraint=c.oid AND (
           NOT t.tgisinternal OR t.tgenabled<>'O' OR t.tgparentid<>0 OR t.tgnargs<>0 OR t.tgqual IS NOT NULL OR
           t.tgfoid NOT IN ('pg_catalog."RI_FKey_check_ins"()'::regprocedure,'pg_catalog."RI_FKey_check_upd"()'::regprocedure,
             'pg_catalog."RI_FKey_noaction_del"()'::regprocedure,'pg_catalog."RI_FKey_noaction_upd"()'::regprocedure)
         )
       )
     )
   ) THEN
     RAISE EXCEPTION 'session workspace foreign key enforcement changed';
   END IF;
   IF NOT EXISTS (SELECT 1 FROM pg_class r WHERE r.oid=v_relation AND r.relkind='r' AND r.relpersistence='p'
       AND pg_get_userbyid(r.relowner)='control_plane_owner' AND NOT r.relispartition
       AND NOT r.relrowsecurity AND NOT r.relforcerowsecurity)
     OR EXISTS (SELECT 1 FROM pg_inherits WHERE inhrelid=v_relation OR inhparent=v_relation)
     OR EXISTS (SELECT 1 FROM pg_attribute WHERE attrelid=v_relation AND attnum>0 AND attacl IS NOT NULL)
     OR EXISTS (SELECT 1 FROM unnest(ARRAY['SELECT','INSERT','UPDATE','DELETE']) privilege
       WHERE NOT has_table_privilege('control_plane_runtime',v_relation,privilege))
     OR EXISTS (SELECT 1 FROM pg_trigger WHERE tgrelid=v_relation AND NOT tgisinternal AND
       (tgconstraint<>0 OR tgparentid<>0 OR octet_length(tgargs)<>0 OR tgoldtable IS NOT NULL OR tgnewtable IS NOT NULL))
     OR EXISTS (SELECT 1 FROM pg_class r CROSS JOIN LATERAL aclexplode(coalesce(r.relacl,acldefault('r',r.relowner))) acl
       WHERE r.oid=v_relation AND (acl.grantee NOT IN ('control_plane_owner'::regrole,'control_plane_runtime'::regrole)
         OR acl.grantee='control_plane_runtime'::regrole AND (acl.is_grantable OR acl.privilege_type NOT IN ('SELECT','INSERT','UPDATE','DELETE'))))
     OR NOT has_function_privilege('control_plane_runtime','control_plane.guard_session_workspace_storage()','EXECUTE')
     OR EXISTS (SELECT 1 FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl
       WHERE p.oid='control_plane.guard_session_workspace_storage()'::regprocedure AND (
         acl.grantee NOT IN ('control_plane_owner'::regrole,'control_plane_runtime'::regrole) OR
         acl.grantee='control_plane_runtime'::regrole AND acl.is_grantable)) THEN
     RAISE EXCEPTION 'session workspace authority changed';
   END IF;
END $workspace_catalog$;
-- +goose StatementEnd

-- Frozen016 archive/task metadata; no business rows.
-- +goose StatementBegin
DO $checkpoint_catalog$
DECLARE v_actual jsonb; v_path text;
BEGIN
 v_path:=current_setting('search_path'); PERFORM set_config('search_path','pg_catalog',true);
WITH targets AS (
 SELECT c.* FROM pg_class c WHERE c.oid IN ('control_plane.session_archives'::regclass,'control_plane.session_archive_tasks'::regclass)
), catalogs AS (
 SELECT r.relname,jsonb_build_object(
 'relation',jsonb_build_array(pg_get_userbyid(r.relowner),r.relkind,r.relpersistence,r.relrowsecurity,r.relforcerowsecurity,r.relispartition,
   EXISTS(SELECT 1 FROM pg_inherits WHERE inhrelid=r.oid OR inhparent=r.oid)),
 'columns',(SELECT jsonb_agg(jsonb_build_array(a.attname,format_type(a.atttypid,a.atttypmod),a.attnotnull,pg_get_expr(d.adbin,d.adrelid),a.attidentity,a.attgenerated,a.attacl::text) ORDER BY a.attnum)
   FROM pg_attribute a LEFT JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum WHERE a.attrelid=r.oid AND a.attnum>0 AND NOT a.attisdropped),
 'constraints',(SELECT jsonb_agg(jsonb_build_array(c.conname,c.contype,c.convalidated,c.conenforced,c.condeferrable,c.condeferred,pg_get_constraintdef(c.oid)) ORDER BY c.conname)
   FROM pg_constraint c WHERE c.conrelid=r.oid),
 'indexes',(SELECT jsonb_agg(jsonb_build_array(c.relname,i.indisunique,i.indisvalid,i.indisready,pg_get_indexdef(i.indexrelid)) ORDER BY c.relname)
   FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid WHERE i.indrelid=r.oid),
 'triggers',(SELECT jsonb_agg(jsonb_build_array(CASE WHEN t.tgisinternal THEN regexp_replace(t.tgname,'_[0-9]+$','_<oid>') ELSE t.tgname END,
   t.tgtype,t.tgenabled,t.tgisinternal,t.tgdeferrable,t.tginitdeferred,t.tgnargs,t.tgattr::text,pg_get_expr(t.tgqual,t.tgrelid),t.tgfoid::regprocedure::text,
   c.conname,c.conrelid::regclass::text,c.confrelid::regclass::text,t.tgparentid=0,encode(t.tgargs,'hex'),t.tgoldtable,t.tgnewtable)
   ORDER BY t.tgisinternal,coalesce(c.conrelid::regclass::text,''),coalesce(c.conname,''),t.tgfoid::regprocedure::text,t.tgtype)
   FROM pg_trigger t LEFT JOIN pg_constraint c ON c.oid=t.tgconstraint WHERE t.tgrelid=r.oid),
 'functions',(SELECT jsonb_agg(jsonb_build_array(p.oid::regprocedure::text,md5(p.prosrc),pg_get_userbyid(p.proowner),p.prosecdef,p.provolatile,p.proconfig,p.prorettype::regtype::text,l.lanname,
   (SELECT jsonb_agg(jsonb_build_array(pg_get_userbyid(a.grantee),a.privilege_type,a.is_grantable) ORDER BY pg_get_userbyid(a.grantee),a.privilege_type)
     FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a)) ORDER BY p.oid::regprocedure::text)
   FROM pg_proc p JOIN pg_language l ON l.oid=p.prolang WHERE p.oid IN(SELECT tgfoid FROM pg_trigger WHERE tgrelid=r.oid AND NOT tgisinternal)),
 'acl',(SELECT jsonb_agg(jsonb_build_array(pg_get_userbyid(a.grantee),a.privilege_type,a.is_grantable) ORDER BY pg_get_userbyid(a.grantee),a.privilege_type)
   FROM aclexplode(coalesce(r.relacl,acldefault('r',r.relowner))) a)
 ) AS metadata FROM targets r
)
SELECT jsonb_build_object('catalog',jsonb_object_agg(relname,metadata),
 'fingerprints',jsonb_object_agg(relname,(SELECT jsonb_object_agg(key,md5(value::text)) FROM jsonb_each(metadata)))) FROM catalogs INTO v_actual;
 PERFORM set_config('search_path',v_path,true);
 IF v_actual->'fingerprints' IS DISTINCT FROM '{"session_archive_tasks":{"acl":"4a534d150446bd64168071defd5f0aa7","columns":"1a6da4ebcea2ea42bbf0cd28167e4703","constraints":"9d7d1e3cf44203a3074b79897956b0e4","functions":"7510b7891aee56e5c335c887b6d28cd0","indexes":"40b47aec851830095254d3bd063950ec","relation":"cdefe876d5d2436e0650dc9eda73dca7","triggers":"745508c173624c3c7a570f317e6cfc9f"},"session_archives":{"acl":"4a534d150446bd64168071defd5f0aa7","columns":"0f3eb233545271dbb0025189e52de4cc","constraints":"fcb1a8510b252554b75f7997525f0e9e","functions":"75af32b813834850a637f2c4c204bd93","indexes":"791ff67359bbb680c6ab28e5024d53f7","relation":"cdefe876d5d2436e0650dc9eda73dca7","triggers":"9a37f41bc0064a13003f2ac68c74d697"}}'::jsonb THEN
   RAISE EXCEPTION 'workspace checkpoint archive catalog changed';
 END IF;
END $checkpoint_catalog$;
-- +goose StatementEnd

-- Точный017 каталог, включая parent-side FK и origin-close triggers; business rows не читаются.
-- +goose StatementBegin
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
-- +goose StatementEnd

CREATE UNIQUE INDEX session_archive_tasks_workspace_restore
 ON control_plane.session_archive_tasks(workspace_id,workspace_generation)
 WHERE workspace_id IS NOT NULL AND kind='RESTORE';
ALTER TABLE control_plane.session_workspace_storage
 DROP CONSTRAINT session_workspace_storage_block_reason_check,
 ADD CONSTRAINT session_workspace_storage_block_reason_check CHECK
 (block_reason IN ('','POD_MISSING','POD_UNREACHABLE','PVC_MISSING','PVC_REPLACED','OWNERSHIP_CHANGED','WRITER_JOIN_UNCONFIRMED','RESTORE_UNCONFIRMED'));

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.guard_workspace_archive_task() RETURNS trigger
LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog AS $$
DECLARE v_expected jsonb; v_checkpoint text;
BEGIN
 IF TG_OP='DELETE' THEN
   IF OLD.workspace_id IS NOT NULL AND (NOT control_plane.project_purge_authorized() OR OLD.state IN ('READY','CLAIMED')) THEN
     RAISE EXCEPTION 'workspace archive task custody is retained';
   END IF;
   RETURN OLD;
 END IF;
 IF TG_OP='UPDATE' THEN
   IF ROW(NEW.workspace_id,NEW.workspace_generation,NEW.workspace_input) IS DISTINCT FROM ROW(OLD.workspace_id,OLD.workspace_generation,OLD.workspace_input) THEN
     RAISE EXCEPTION 'workspace archive task profile is immutable';
   END IF;
   IF NEW.workspace_id IS NULL THEN RETURN NEW; END IF;
   IF ROW(NEW.id,NEW.ref,NEW.organization_id,NEW.project_id,NEW.session_id,NEW.archive_id,NEW.kind,NEW.content_generation,NEW.object_key,NEW.object_version,NEW.maximum_attempts,NEW.created_at)
      IS DISTINCT FROM ROW(OLD.id,OLD.ref,OLD.organization_id,OLD.project_id,OLD.session_id,OLD.archive_id,OLD.kind,OLD.content_generation,OLD.object_key,OLD.object_version,OLD.maximum_attempts,OLD.created_at) THEN
     RAISE EXCEPTION 'workspace archive task identity is immutable';
   END IF;
   IF OLD.state IN ('SUCCEEDED','CANCELLED','DEAD_LETTER') AND
     ROW(NEW.state,NEW.attempt,NEW.generation,NEW.workload_instance,NEW.lease_ref,NEW.fence_digest,NEW.lease_expires_at,NEW.input_digest,NEW.safe_error_code,NEW.completed_at,NEW.available_at)
     IS DISTINCT FROM ROW(OLD.state,OLD.attempt,OLD.generation,OLD.workload_instance,OLD.lease_ref,OLD.fence_digest,OLD.lease_expires_at,OLD.input_digest,OLD.safe_error_code,OLD.completed_at,OLD.available_at) THEN
     RAISE EXCEPTION 'workspace archive terminal task is immutable';
   END IF;
   IF NEW.state='CLAIMED' THEN
     IF NEW.generation=OLD.generation THEN
       IF OLD.state<>'CLAIMED' OR ROW(NEW.attempt,NEW.workload_instance,NEW.lease_ref,NEW.fence_digest,NEW.input_digest)
          IS DISTINCT FROM ROW(OLD.attempt,OLD.workload_instance,OLD.lease_ref,OLD.fence_digest,OLD.input_digest)
          OR NEW.lease_expires_at<OLD.lease_expires_at THEN
         RAISE EXCEPTION 'workspace archive renewal changed its grant'; END IF;
     ELSIF NEW.generation<>OLD.generation+1 OR NEW.attempt<>OLD.attempt+1 OR
       NOT(OLD.state='READY' AND OLD.available_at<=clock_timestamp() OR OLD.state='CLAIMED' AND OLD.lease_expires_at<=clock_timestamp()) THEN
       RAISE EXCEPTION 'workspace archive claim generation is invalid';
     END IF;
   ELSIF ROW(NEW.attempt,NEW.generation,NEW.input_digest) IS DISTINCT FROM ROW(OLD.attempt,OLD.generation,OLD.input_digest) OR
     NEW.state<>OLD.state AND NOT(OLD.state='CLAIMED' AND NEW.state IN ('READY','SUCCEEDED','CANCELLED','DEAD_LETTER') OR OLD.state='READY' AND NEW.state='CANCELLED') THEN
     RAISE EXCEPTION 'workspace archive task transition is invalid';
   END IF;
   RETURN NEW;
 END IF;
 IF NEW.workspace_id IS NULL THEN RETURN NEW; END IF;
 IF NEW.kind='RESTORE' THEN
   IF NEW.state<>'READY' OR NEW.attempt<>0 OR NEW.generation<>0 OR NEW.archive_id IS NULL OR NEW.maximum_attempts<>5 THEN
     RAISE EXCEPTION 'workspace restore task initial state is invalid';
   END IF;
   SELECT jsonb_build_object(
     'target',jsonb_build_object('profile','SESSION_WORKSPACE_V2',
       'scope',jsonb_build_object('organization_ref',o.ref,'project_ref',p.ref,'session_ref',s.ref,'agent_ref',a.ref),
       'workspace_ref',w.ref,'generation',w.generation,'volume_ready',v.ready_record::jsonb),
     'checkpoint_ref',source.workspace_checkpoint_ref,'pvc_uid',v.pvc_uid,'reconcile_only',false,
     'policy',jsonb_build_object('revision',r.safe_snapshot #> '{workspacePolicy,revision}',
       'root',r.safe_snapshot #> '{workspacePolicy,root}','rules',r.safe_snapshot #> '{workspacePolicy,rules}',
       'maximum_writable_bytes',r.safe_snapshot #> '{workspacePolicy,maximumWritableBytes}',
       'maximum_file_count',r.safe_snapshot #> '{workspacePolicy,maximumFileCount}',
       'digest',r.safe_snapshot #> '{workspacePolicy,digest}','denial_reasons',r.safe_snapshot #> '{workspacePolicy,denialReasons}'),
     'checkpoint',jsonb_build_object(
       'scope',jsonb_build_object('organization_ref',o.ref,'project_ref',p.ref,'session_ref',s.ref,'agent_ref',a.ref),
       'workspace_ref',w.ref,'generation',source.workspace_generation,'checkpoint_ref',source.workspace_checkpoint_ref,
       'tree_sha256',source.source_sha256,'bytes',source.source_size_bytes,'entries',source.workspace_entries,
       'manifest',jsonb_build_object('key',source.object_key,'version',source.object_version,'etag',source.object_etag,
         'digest',source.object_digest,'size_bytes',source.object_size_bytes))) INTO v_expected
   FROM control_plane.session_workspace_storage w
   JOIN control_plane.sessions s ON s.id=w.session_id AND s.organization_id=w.organization_id AND s.project_id=w.project_id
   JOIN control_plane.agents a ON a.id=w.agent_id AND a.organization_id=w.organization_id AND a.project_id=w.project_id AND a.ref=s.target_ref
   JOIN control_plane.projects p ON p.id=w.project_id AND p.organization_id=w.organization_id
   JOIN control_plane.organizations o ON o.id=w.organization_id
   JOIN control_plane.session_archives source ON source.id=w.source_archive_id AND source.workspace_id=w.id
     AND source.organization_id=w.organization_id AND source.project_id=w.project_id AND source.session_id=w.session_id
   JOIN control_plane.runtime_revisions r ON r.id=source.runtime_revision_id AND r.organization_id=w.organization_id
     AND r.project_id=w.project_id AND r.session_id=w.session_id AND r.agent_id=w.agent_id
   JOIN control_plane.session_workspace_volumes v ON v.workspace_id=w.id
   WHERE w.id=NEW.workspace_id AND w.organization_id=NEW.organization_id AND w.project_id=NEW.project_id
     AND w.session_id=NEW.session_id AND w.generation=NEW.workspace_generation
     AND w.state='RESTORE_READY' AND w.writer_lease_id IS NULL AND NOT w.writer_released
     AND w.writer_acknowledgement IS NULL AND w.quiescence_record IS NULL AND w.destructive_task_id IS NULL
     AND source.id=NEW.archive_id AND source.format_version=2 AND source.lifecycle_state='AVAILABLE'
     AND source.deleted_at IS NULL AND source.workspace_generation=w.generation-1
     AND v.state='READY' AND v.pvc_uid IS NOT NULL AND v.ready_record IS NOT NULL AND NOT v.physical_deleted
     AND p.lifecycle IN ('ACTIVE','TRASHED') AND r.safe_snapshot->'roleRuntimeContractRevision'='4'::jsonb
     AND NEW.object_key=source.object_key AND NEW.object_version=source.object_version;
   IF v_expected IS NULL OR NEW.workspace_input IS DISTINCT FROM v_expected THEN
     RAISE EXCEPTION 'workspace restore task source is invalid';
   END IF;
   RETURN NEW;
 END IF;
 IF NEW.kind<>'SNAPSHOT' OR NEW.state<>'READY' OR NEW.attempt<>0 OR NEW.generation<>0 OR NEW.archive_id IS NOT NULL OR NEW.maximum_attempts<>5 THEN
   RAISE EXCEPTION 'workspace archive task initial state is invalid';
 END IF;
 v_checkpoint:=NEW.workspace_input->>'checkpoint_ref';
 SELECT jsonb_build_object(
   'target',jsonb_build_object('profile','SESSION_WORKSPACE_V2','scope',jsonb_build_object('organization_ref',o.ref,'project_ref',p.ref,'session_ref',s.ref,'agent_ref',a.ref),
     'workspace_ref',w.ref,'generation',w.generation),
   'checkpoint_ref',v_checkpoint,'quiescence',w.quiescence_record,'pvc_uid',w.writer_acknowledgement::jsonb #>> '{writer,pvc_uid}',
   'reconcile_only',false,'policy',jsonb_build_object('revision',r.safe_snapshot #> '{workspacePolicy,revision}','root',r.safe_snapshot #> '{workspacePolicy,root}',
     'rules',r.safe_snapshot #> '{workspacePolicy,rules}','maximum_writable_bytes',r.safe_snapshot #> '{workspacePolicy,maximumWritableBytes}',
     'maximum_file_count',r.safe_snapshot #> '{workspacePolicy,maximumFileCount}','digest',r.safe_snapshot #> '{workspacePolicy,digest}',
     'denial_reasons',r.safe_snapshot #> '{workspacePolicy,denialReasons}')) INTO v_expected
 FROM control_plane.session_workspace_storage w
 JOIN control_plane.sessions s ON s.id=w.session_id AND s.organization_id=w.organization_id AND s.project_id=w.project_id
 JOIN control_plane.agents a ON a.id=w.agent_id AND a.organization_id=w.organization_id AND a.project_id=w.project_id AND a.ref=s.target_ref
 JOIN control_plane.projects p ON p.id=w.project_id AND p.organization_id=w.organization_id
 JOIN control_plane.organizations o ON o.id=w.organization_id
 JOIN control_plane.runtime_leases l ON l.id=w.writer_lease_id AND l.organization_id=w.organization_id
 JOIN control_plane.runtime_revisions r ON r.id=l.runtime_revision_id AND r.session_id=w.session_id AND r.organization_id=w.organization_id
   AND r.project_id=w.project_id AND r.agent_id=w.agent_id
 WHERE w.id=NEW.workspace_id AND w.organization_id=NEW.organization_id AND w.project_id=NEW.project_id AND w.session_id=NEW.session_id
   AND w.generation=NEW.workspace_generation AND w.state='CHECKPOINT_PENDING' AND w.writer_released
   AND w.quiescence_record IS NOT NULL AND w.current_archive_id IS NULL AND r.safe_snapshot->'roleRuntimeContractRevision'='4'::jsonb
   AND p.lifecycle IN ('ACTIVE','TRASHED')
   AND NEW.object_key='session-archive/v2/'||o.ref||'/'||p.ref||'/'||s.ref||'/'||w.ref||'/g'||w.generation::text||'/'||v_checkpoint||'/manifest.json';
 IF v_expected IS NULL OR NEW.workspace_input IS DISTINCT FROM v_expected OR (v_checkpoint ~ '^wcp_[A-Za-z0-9_-]{8,89}$') IS NOT TRUE THEN
   RAISE EXCEPTION 'workspace archive task source is invalid';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
RESET ROLE;

-- +goose Down
-- Принятые restore intent и source custody сохраняются при binary rollback.
SELECT 1;
