-- +goose Up
SET LOCAL lock_timeout='8s';
SET LOCAL statement_timeout='30s';
-- +goose StatementBegin
DO $predecessor$
BEGIN
 IF (SELECT max(version_id) FROM public.goose_db_version WHERE is_applied) IS DISTINCT FROM 20261026001500::bigint THEN
   RAISE EXCEPTION 'workspace checkpoint requires exact profile015';
 END IF;
END $predecessor$;
-- +goose StatementEnd
SET ROLE control_plane_owner;

-- Frozen015 workspace catalog; runtime не читает внешний файл.
-- +goose StatementBegin
-- Read-only точный каталог workspace; не читает business rows или секреты.
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

-- +goose StatementBegin
-- Frozen015 archive/task metadata; no business rows.
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
 IF v_actual->'fingerprints' IS DISTINCT FROM '{"session_archive_tasks":{"acl":"4a534d150446bd64168071defd5f0aa7","columns":"f3cbc24750758ad40dd5f8c502d23dbe","constraints":"1be4f694414c710b9d5c7af598cd27db","functions":"37a6259cc0c1dae299a7866489dff0bd","indexes":"4cf010f3fd87ac462843d83d003726b4","relation":"cdefe876d5d2436e0650dc9eda73dca7","triggers":"8331df0639de4e264fc648e83bbcace3"},"session_archives":{"acl":"4a534d150446bd64168071defd5f0aa7","columns":"aeff8a879216d033e5511dd8a5f38236","constraints":"ecdd38f111e0e01fd0a771f2c7e80663","functions":"9cd820ff9c9e844b48fab3640f34a7f6","indexes":"b58c0b62df318e3d1c22c387e8696a31","relation":"cdefe876d5d2436e0650dc9eda73dca7","triggers":"a7c1b6f4b0ab976bdbb1fc7984050242"}}'::jsonb THEN
   RAISE EXCEPTION 'workspace checkpoint predecessor archive catalog changed';
 END IF;
END $checkpoint_catalog$;
-- +goose StatementEnd

-- Разделение форматов сохраняет publication key и каждый прежний v1 reader.
-- Рабочее дерево до запуска модели не получает выдуманный Codex session ID.
ALTER TABLE control_plane.session_archives
 ALTER COLUMN codex_session_id DROP NOT NULL,
 ALTER COLUMN source_relative_path DROP NOT NULL,
 DROP CONSTRAINT session_archives_format_version_check,
 DROP CONSTRAINT session_archives_source_size_bytes_check,
 ADD COLUMN workspace_id uuid REFERENCES control_plane.session_workspace_storage(id) DEFERRABLE INITIALLY IMMEDIATE,
 ADD COLUMN workspace_generation bigint,
 ADD COLUMN workspace_checkpoint_ref text,
 ADD COLUMN workspace_entries bigint,
 ADD COLUMN workspace_quiescence text,
 ADD CONSTRAINT session_archives_format_shape CHECK (
   (format_version=1 AND codex_session_id IS NOT NULL AND source_relative_path IS NOT NULL
    AND source_size_bytes BETWEEN 1 AND 67108864 AND workspace_id IS NULL AND workspace_generation IS NULL
    AND workspace_checkpoint_ref IS NULL AND workspace_entries IS NULL AND workspace_quiescence IS NULL)
   OR (format_version=2 AND codex_session_id IS NULL AND source_relative_path IS NULL AND project_id IS NOT NULL
    AND source_size_bytes BETWEEN 0 AND 1073741824 AND workspace_id IS NOT NULL AND workspace_generation IS NOT NULL AND workspace_generation>0
    AND workspace_checkpoint_ref IS NOT NULL AND workspace_checkpoint_ref ~ '^wcp_[A-Za-z0-9_-]{8,89}$'
    AND workspace_entries IS NOT NULL AND workspace_entries BETWEEN 0 AND 100000
    AND (source_size_bytes=0 OR workspace_entries>0) AND workspace_quiescence IS NOT NULL AND octet_length(workspace_quiescence) BETWEEN 1 AND 2048
    AND object_size_bytes<=65536 AND char_length(object_version)>0));
CREATE UNIQUE INDEX session_archives_workspace_checkpoint ON control_plane.session_archives(workspace_checkpoint_ref) WHERE format_version=2;
CREATE UNIQUE INDEX session_archives_workspace_generation ON control_plane.session_archives(workspace_id,workspace_generation) WHERE format_version=2;

ALTER TABLE control_plane.session_archive_tasks
 ADD COLUMN workspace_id uuid REFERENCES control_plane.session_workspace_storage(id) DEFERRABLE INITIALLY IMMEDIATE,
 ADD COLUMN workspace_generation bigint,
 ADD COLUMN workspace_input jsonb,
 ADD CONSTRAINT session_archive_tasks_workspace_shape CHECK (
   (workspace_id IS NULL AND workspace_generation IS NULL AND workspace_input IS NULL)
   OR (workspace_id IS NOT NULL AND workspace_generation IS NOT NULL AND workspace_generation>0
       AND workspace_input IS NOT NULL AND jsonb_typeof(workspace_input)='object' AND octet_length(workspace_input::text)<=65536
       AND attempt<=5 AND generation>=attempt AND ((state IN ('SUCCEEDED','CANCELLED','DEAD_LETTER'))=(completed_at IS NOT NULL))));
CREATE UNIQUE INDEX session_archive_tasks_workspace_snapshot ON control_plane.session_archive_tasks(workspace_id,workspace_generation)
 WHERE workspace_id IS NOT NULL AND kind='SNAPSHOT';

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_workspace_archive_task() RETURNS trigger
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
REVOKE ALL ON FUNCTION control_plane.guard_workspace_archive_task() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.guard_workspace_archive_task() TO control_plane_runtime;
CREATE TRIGGER workspace_archive_task_guard BEFORE INSERT OR UPDATE OR DELETE ON control_plane.session_archive_tasks
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_workspace_archive_task();

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_workspace_archive_receipt() RETURNS trigger
LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog AS $$
BEGIN
 IF TG_OP='DELETE' THEN
   IF OLD.format_version=2 AND (NOT control_plane.project_purge_authorized() OR OLD.lifecycle_state<>'DELETED') THEN
     RAISE EXCEPTION 'workspace archive receipt custody is retained';
   END IF;
   RETURN OLD;
 END IF;
 IF TG_OP='UPDATE' THEN
   IF ROW(NEW.workspace_id,NEW.workspace_generation,NEW.workspace_checkpoint_ref,NEW.workspace_entries,NEW.workspace_quiescence)
       IS DISTINCT FROM ROW(OLD.workspace_id,OLD.workspace_generation,OLD.workspace_checkpoint_ref,OLD.workspace_entries,OLD.workspace_quiescence) THEN
     RAISE EXCEPTION 'workspace archive receipt metadata is immutable';
   END IF;
   RETURN NEW;
 END IF;
 IF NEW.format_version<>2 THEN RETURN NEW; END IF;
 IF NOT EXISTS (
   SELECT 1 FROM control_plane.session_workspace_storage w
   JOIN control_plane.runtime_leases l ON l.id=w.writer_lease_id AND l.organization_id=w.organization_id
   JOIN control_plane.runtime_revisions r ON r.id=l.runtime_revision_id AND r.organization_id=w.organization_id
     AND r.session_id=w.session_id AND r.project_id=w.project_id AND r.agent_id=w.agent_id
   JOIN control_plane.session_archive_tasks t ON t.workspace_id=w.id AND t.organization_id=w.organization_id
     AND t.project_id=w.project_id AND t.session_id=w.session_id AND t.workspace_generation=w.generation
     AND t.kind='SNAPSHOT' AND t.state='CLAIMED' AND t.lease_expires_at>clock_timestamp()
   WHERE w.id=NEW.workspace_id AND w.organization_id=NEW.organization_id AND w.project_id=NEW.project_id AND w.session_id=NEW.session_id
     AND w.generation=NEW.workspace_generation AND w.state='CHECKPOINT_PENDING' AND w.writer_released
     AND w.quiescence_record=NEW.workspace_quiescence AND w.current_archive_id IS NULL
     AND r.id=NEW.runtime_revision_id AND r.provider_account_id=NEW.provider_account_id
     AND t.content_generation=NEW.content_generation AND t.object_key=NEW.object_key AND t.workspace_input->>'checkpoint_ref'=NEW.workspace_checkpoint_ref
     AND NEW.source_size_bytes<=(t.workspace_input #>> '{policy,maximum_writable_bytes}')::bigint
     AND NEW.workspace_entries<=(t.workspace_input #>> '{policy,maximum_file_count}')::bigint
     AND NEW.lifecycle_state='AVAILABLE' AND NEW.deleted_at IS NULL
 ) THEN RAISE EXCEPTION 'workspace archive receipt source is invalid'; END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_workspace_archive_receipt() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.guard_workspace_archive_receipt() TO control_plane_runtime;
CREATE TRIGGER workspace_archive_receipt_guard BEFORE INSERT OR UPDATE OR DELETE ON control_plane.session_archives
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_workspace_archive_receipt();

-- +goose StatementBegin
DO $purge$
DECLARE v_definition text;v_source text;v_old_edges text;v_old_nodes text;v_nodes oid[];v_count integer;v_digest text;
BEGIN
 SELECT pg_get_functiondef(p.oid),p.prosrc INTO STRICT v_definition,v_source FROM pg_proc p
 WHERE p.oid='control_plane.purge_project_database(uuid,uuid,text)'::regprocedure
 AND p.proowner='control_plane_owner'::regrole AND p.prosecdef AND p.provolatile='v'
 AND p.prorettype='integer'::regtype AND NOT p.proretset AND p.proconfig=ARRAY['search_path=pg_catalog']
 AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql')
 AND has_function_privilege('control_plane_runtime',p.oid,'EXECUTE')
 AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE');
 v_old_edges:=substring(v_source FROM 'IF v_edges <> 294 OR v_edge_fingerprint <> ''[a-f0-9]{32}'' THEN');
 v_old_nodes:=substring(v_source FROM 'IF v_nodes <> 106 OR v_node_fingerprint <> ''[a-f0-9]{32}'' THEN');
 IF v_old_edges IS NULL OR v_old_nodes IS NULL OR md5(replace(replace(v_source,v_old_edges,
   'IF v_edges <> 284 OR v_edge_fingerprint <> ''6bf6ad76d8c7ae66e07f75ecf719fdd6'' THEN'),v_old_nodes,
   'IF v_nodes <> 105 OR v_node_fingerprint <> ''f51a0de25bd942a250997775617eeaee'' THEN'))<>'b886c24d4e2f5c4a0ec08123d99462b0' THEN
   RAISE EXCEPTION 'workspace checkpoint purge preimage changed'; END IF;
 WITH RECURSIVE nodes(id) AS (SELECT 'control_plane.projects'::regclass::oid UNION SELECT c.conrelid FROM nodes n JOIN pg_constraint c ON c.confrelid=n.id
   WHERE c.contype='f' AND c.connamespace='control_plane'::regnamespace) SELECT array_agg(id) INTO v_nodes FROM nodes;
 SELECT count(*),md5(string_agg(n.nspname||'.'||r.relname,E'\n' ORDER BY n.nspname,r.relname)) INTO v_count,v_digest
 FROM pg_class r JOIN pg_namespace n ON n.oid=r.relnamespace WHERE r.oid=ANY(v_nodes);
 IF v_count<>106 OR v_old_nodes IS DISTINCT FROM format('IF v_nodes <> %s OR v_node_fingerprint <> %L THEN',v_count,v_digest) THEN
   RAISE EXCEPTION 'workspace checkpoint predecessor nodes changed'; END IF;
 SELECT count(*),md5(string_agg(cn.nspname||'.'||cc.relname||'|'||c.conname||'|'||pn.nspname||'.'||pc.relname||'|'||c.confdeltype::text,E'\n' ORDER BY cn.nspname,cc.relname,c.conname))
 INTO v_count,v_digest FROM pg_constraint c JOIN pg_class cc ON cc.oid=c.conrelid JOIN pg_namespace cn ON cn.oid=cc.relnamespace
 JOIN pg_class pc ON pc.oid=c.confrelid JOIN pg_namespace pn ON pn.oid=pc.relnamespace
 WHERE c.contype='f' AND c.conrelid=ANY(v_nodes) AND c.confrelid=ANY(v_nodes)
 AND NOT(c.conrelid='control_plane.session_archives'::regclass AND c.conname='session_archives_workspace_id_fkey'
     AND c.confrelid='control_plane.session_workspace_storage'::regclass)
 AND NOT(c.conrelid='control_plane.session_archive_tasks'::regclass AND c.conname='session_archive_tasks_workspace_id_fkey'
     AND c.confrelid='control_plane.session_workspace_storage'::regclass);
 IF v_count<>294 OR v_old_edges IS DISTINCT FROM format('IF v_edges <> %s OR v_edge_fingerprint <> %L THEN',v_count,v_digest) THEN
   RAISE EXCEPTION 'workspace checkpoint predecessor graph changed'; END IF;
 SELECT count(*),md5(string_agg(cn.nspname||'.'||cc.relname||'|'||c.conname||'|'||pn.nspname||'.'||pc.relname||'|'||c.confdeltype::text,E'\n' ORDER BY cn.nspname,cc.relname,c.conname))
 INTO v_count,v_digest FROM pg_constraint c JOIN pg_class cc ON cc.oid=c.conrelid JOIN pg_namespace cn ON cn.oid=cc.relnamespace
 JOIN pg_class pc ON pc.oid=c.confrelid JOIN pg_namespace pn ON pn.oid=pc.relnamespace
 WHERE c.contype='f' AND c.conrelid=ANY(v_nodes) AND c.confrelid=ANY(v_nodes);
 IF cardinality(v_nodes)<>106 OR v_count<>296 THEN RAISE EXCEPTION 'workspace checkpoint graph size changed'; END IF;
 EXECUTE replace(v_definition,v_old_edges,format('IF v_edges <> %s OR v_edge_fingerprint <> %L THEN',v_count,v_digest));
END $purge$;
-- +goose StatementEnd
RESET ROLE;

-- +goose Down
-- Checkpoint metadata не удаляются ради binary rollback.
SELECT 1;
