-- +goose Up
SET LOCAL lock_timeout='8s';
SET LOCAL statement_timeout='30s';
-- +goose StatementBegin
DO $predecessor$
BEGIN
 IF (SELECT max(version_id) FROM public.goose_db_version WHERE is_applied)
    IS DISTINCT FROM 20261026001600::bigint THEN
   RAISE EXCEPTION 'workspace volume custody requires exact profile016';
 END IF;
END $predecessor$;
-- +goose StatementEnd
SET ROLE control_plane_owner;

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

-- Точный frozen016 predecessor до добавления новых таблиц; business rows не читаются.
-- +goose StatementBegin
-- Frozen016 archive/task metadata; no business rows.
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

-- Physical incarnation существует до model RR. Work generation не является PVC identity.
CREATE TABLE control_plane.session_workspace_volumes (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 ref text NOT NULL UNIQUE CHECK(ref ~ '^wvl_[A-Za-z0-9_-]{8,89}$'),
 workspace_id uuid NOT NULL UNIQUE REFERENCES control_plane.session_workspace_storage(id) DEFERRABLE INITIALLY IMMEDIATE,
 generation bigint NOT NULL DEFAULT 1 CHECK(generation>0),
 state text NOT NULL CHECK(state IN('CREATE_PENDING','CREATED','PREPARE_PENDING','READY','BLOCKED','PURGED')),
 create_task_ref text NOT NULL CHECK(create_task_ref ~ '^svt_[A-Za-z0-9_-]{8,89}$'),
 preparation_task_ref text CHECK(preparation_task_ref IS NULL OR preparation_task_ref ~ '^svt_[A-Za-z0-9_-]{8,89}$'),
 pvc_uid text CHECK(pvc_uid IS NULL OR pvc_uid ~ '^[a-z0-9][a-z0-9-]{0,127}$'),
 ready_record text CHECK(ready_record IS NULL OR
   octet_length(ready_record) BETWEEN 1 AND 2048 AND ready_record IS JSON OBJECT WITH UNIQUE KEYS),
 physical_deleted boolean NOT NULL DEFAULT false,
 block_reason text NOT NULL DEFAULT '' CHECK(block_reason IN('','ORIGIN_CLOSED','CREATE_UNCONFIRMED','PREPARE_UNCONFIRMED',
   'PVC_MISSING','PVC_REPLACED','OWNERSHIP_CHANGED','STORAGE_UNAVAILABLE')),
 version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CONSTRAINT session_workspace_volume_block_shape CHECK((state='BLOCKED')=(block_reason<>'')),
 CONSTRAINT session_workspace_volume_uid_shape CHECK(state NOT IN('CREATED','PREPARE_PENDING','READY') OR pvc_uid IS NOT NULL),
 CONSTRAINT session_workspace_volume_ready_shape CHECK(state<>'READY' OR ready_record IS NOT NULL AND preparation_task_ref IS NOT NULL),
 CONSTRAINT session_workspace_volume_purge_shape CHECK((state='PURGED')=physical_deleted)
);
CREATE UNIQUE INDEX session_workspace_volume_positive_uid
 ON control_plane.session_workspace_volumes(pvc_uid) WHERE pvc_uid IS NOT NULL;

CREATE TABLE control_plane.session_workspace_volume_tasks (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 ref text NOT NULL UNIQUE CHECK(ref ~ '^svt_[A-Za-z0-9_-]{8,89}$'),
 volume_id uuid NOT NULL REFERENCES control_plane.session_workspace_volumes(id) DEFERRABLE INITIALLY IMMEDIATE,
 volume_generation bigint NOT NULL CHECK(volume_generation>0),
 origin_node_id uuid NOT NULL REFERENCES control_plane.run_nodes(id) DEFERRABLE INITIALLY IMMEDIATE,
 root_run_id uuid NOT NULL REFERENCES control_plane.runs(id) DEFERRABLE INITIALLY IMMEDIATE,
 origin_actor_id uuid NOT NULL REFERENCES control_plane.subjects(id) DEFERRABLE INITIALLY IMMEDIATE,
 kind text NOT NULL CHECK(kind IN('CREATE_WORKSPACE_VOLUME','PREPARE_WORKSPACE_VOLUME')),
 state text NOT NULL DEFAULT 'READY' CHECK(state IN('READY','CLAIMED','SUCCEEDED','CANCELLED','DEAD_LETTER')),
 frozen_input jsonb NOT NULL CHECK(jsonb_typeof(frozen_input)='object' AND octet_length(frozen_input::text)<=2048),
 attempt integer NOT NULL DEFAULT 0 CHECK(attempt BETWEEN 0 AND 5),
 maximum_attempts integer NOT NULL DEFAULT 5 CHECK(maximum_attempts=5),
 available_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 safe_error_code text NOT NULL DEFAULT '' CHECK(safe_error_code IN('','ORIGIN_CLOSED','CREATE_UNCONFIRMED',
   'PREPARE_UNCONFIRMED','PVC_MISSING','PVC_REPLACED','OWNERSHIP_CHANGED','STORAGE_UNAVAILABLE')),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 completed_at timestamptz,
 CONSTRAINT session_workspace_volume_task_terminal CHECK(
   (state IN('SUCCEEDED','CANCELLED','DEAD_LETTER'))=(completed_at IS NOT NULL)),
 CONSTRAINT session_workspace_volume_task_claimed CHECK(state<>'CLAIMED' OR attempt>0)
);
CREATE UNIQUE INDEX session_workspace_volume_task_open
 ON control_plane.session_workspace_volume_tasks(volume_id,volume_generation)
 WHERE state IN('READY','CLAIMED');
CREATE INDEX session_workspace_volume_task_candidates
 ON control_plane.session_workspace_volume_tasks(available_at,id)
 WHERE state IN('READY','CLAIMED');
-- Один RW и один RO intent для exact origin: terminal отказ не порождает бесконечные новые logical tasks.
CREATE UNIQUE INDEX session_workspace_volume_prepare_origin
 ON control_plane.session_workspace_volume_tasks(volume_id,volume_generation,origin_node_id,root_run_id,
   origin_actor_id,(frozen_input->>'read_only'))
 WHERE kind='PREPARE_WORKSPACE_VOLUME';
ALTER TABLE control_plane.session_workspace_volumes
 ADD CONSTRAINT session_workspace_volume_create_task
 FOREIGN KEY(create_task_ref) REFERENCES control_plane.session_workspace_volume_tasks(ref) DEFERRABLE INITIALLY DEFERRED,
 ADD CONSTRAINT session_workspace_volume_prepare_task
 FOREIGN KEY(preparation_task_ref) REFERENCES control_plane.session_workspace_volume_tasks(ref) DEFERRABLE INITIALLY DEFERRED;

-- Ledger переживает отзыв lease; secret fence сохраняется только как digest.
CREATE TABLE control_plane.session_workspace_volume_attempts (
 task_id uuid NOT NULL REFERENCES control_plane.session_workspace_volume_tasks(id) DEFERRABLE INITIALLY IMMEDIATE,
 attempt integer NOT NULL CHECK(attempt BETWEEN 1 AND 5),
 lease_ref text NOT NULL UNIQUE CHECK(lease_ref ~ '^lea_[A-Za-z0-9_-]{8,89}$'),
 fence_digest text NOT NULL CHECK(fence_digest ~ '^[a-f0-9]{64}$'),
 workload_instance text NOT NULL CHECK(octet_length(workload_instance) BETWEEN 1 AND 256),
 input_snapshot text NOT NULL CHECK(octet_length(input_snapshot) BETWEEN 1 AND 2048
   AND input_snapshot IS JSON OBJECT WITH UNIQUE KEYS),
 input_digest text NOT NULL CHECK(input_digest ~ '^[a-f0-9]{64}$'),
 grant_state text NOT NULL DEFAULT 'ACTIVE' CHECK(grant_state IN('ACTIVE','CLOSED')),
 expires_at timestamptz NOT NULL,
 observed_pvc_uid text CHECK(observed_pvc_uid IS NULL OR observed_pvc_uid ~ '^[a-z0-9][a-z0-9-]{0,127}$'),
 job_uid text CHECK(job_uid IS NULL OR job_uid ~ '^[a-z0-9][a-z0-9-]{0,127}$'),
 pod_uid text CHECK(pod_uid IS NULL OR pod_uid ~ '^[a-z0-9][a-z0-9-]{0,127}$'),
 ready_record text CHECK(ready_record IS NULL OR
   octet_length(ready_record) BETWEEN 1 AND 2048 AND ready_record IS JSON OBJECT WITH UNIQUE KEYS),
 reported_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(task_id,attempt),
 CONSTRAINT session_workspace_volume_attempt_digest CHECK(input_digest=encode(sha256(convert_to(input_snapshot,'UTF8')),'hex')),
 CONSTRAINT session_workspace_volume_attempt_report CHECK(
   (reported_at IS NOT NULL)=(observed_pvc_uid IS NOT NULL)
   AND (ready_record IS NULL)=(pod_uid IS NULL) AND (job_uid IS NULL)=(pod_uid IS NULL))
);
CREATE UNIQUE INDEX session_workspace_volume_attempt_active
 ON control_plane.session_workspace_volume_attempts(task_id) WHERE grant_state='ACTIVE';

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_workspace_volume_custody() RETURNS trigger
LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog AS $$
DECLARE v_expected jsonb; v_ready jsonb;
BEGIN
 IF TG_OP='DELETE' THEN
   IF NOT control_plane.project_purge_authorized() OR OLD.state<>'PURGED' OR NOT OLD.physical_deleted THEN
     RAISE EXCEPTION 'workspace volume custody is retained';
   END IF;
   RETURN OLD;
 END IF;
 IF TG_OP='INSERT' THEN
   IF NEW.generation<>1 OR NEW.version<>1 OR NEW.state<>'CREATE_PENDING' OR NEW.pvc_uid IS NOT NULL
      OR NEW.preparation_task_ref IS NOT NULL OR NEW.ready_record IS NOT NULL OR NEW.physical_deleted
      OR NOT EXISTS(SELECT 1 FROM control_plane.session_workspace_storage w
        WHERE w.id=NEW.workspace_id AND w.state='EMPTY' AND w.writer_lease_id IS NULL) THEN
     RAISE EXCEPTION 'workspace volume initial custody is invalid';
   END IF;
 ELSE
   IF ROW(NEW.id,NEW.ref,NEW.workspace_id,NEW.created_at) IS DISTINCT FROM ROW(OLD.id,OLD.ref,OLD.workspace_id,OLD.created_at)
      OR NEW.version<>OLD.version+1 THEN
     RAISE EXCEPTION 'workspace volume identity is immutable';
   END IF;
   IF NEW.generation<>OLD.generation THEN
     IF NEW.generation<>OLD.generation+1 OR OLD.state<>'PURGED' OR NOT OLD.physical_deleted OR
        NEW.state<>'CREATE_PENDING' OR NEW.pvc_uid IS NOT NULL OR NEW.ready_record IS NOT NULL OR
        NEW.preparation_task_ref IS NOT NULL OR NEW.physical_deleted OR NEW.create_task_ref=OLD.create_task_ref THEN
       RAISE EXCEPTION 'workspace volume incarnation is not released';
     END IF;
   ELSE
     IF OLD.state='PURGED' OR NEW.create_task_ref<>OLD.create_task_ref OR
        OLD.pvc_uid IS NOT NULL AND NEW.pvc_uid IS DISTINCT FROM OLD.pvc_uid OR
        OLD.preparation_task_ref IS NOT NULL AND NEW.preparation_task_ref IS DISTINCT FROM OLD.preparation_task_ref OR
        OLD.ready_record IS NOT NULL AND NEW.ready_record IS DISTINCT FROM OLD.ready_record THEN
       RAISE EXCEPTION 'workspace volume physical pins are immutable';
     END IF;
     IF NEW.state<>OLD.state AND NOT(
       OLD.state='CREATE_PENDING' AND NEW.state='CREATED' OR
       OLD.state='CREATED' AND NEW.state='PREPARE_PENDING' OR
       OLD.state='PREPARE_PENDING' AND NEW.state='READY' OR
       OLD.state='BLOCKED' AND NEW.state IN('CREATED','PREPARE_PENDING','READY') OR
       NEW.state='BLOCKED' OR NEW.state='PURGED') THEN
       RAISE EXCEPTION 'workspace volume custody transition is invalid';
     END IF;
   END IF;
 END IF;
 IF NEW.pvc_uid IS NOT NULL AND NOT EXISTS(
   SELECT 1 FROM control_plane.session_workspace_volume_tasks t
   JOIN control_plane.session_workspace_volume_attempts a ON a.task_id=t.id
   WHERE t.volume_id=NEW.id AND t.volume_generation=NEW.generation AND t.ref=NEW.create_task_ref
     AND t.kind='CREATE_WORKSPACE_VOLUME' AND a.attempt=1 AND a.grant_state='CLOSED'
     AND a.reported_at IS NOT NULL AND a.observed_pvc_uid=NEW.pvc_uid) THEN
   RAISE EXCEPTION 'workspace volume positive create proof is missing';
 END IF;
 IF NEW.ready_record IS NOT NULL AND NOT EXISTS(
   SELECT 1 FROM control_plane.session_workspace_volume_tasks t
   JOIN control_plane.session_workspace_volume_attempts a ON a.task_id=t.id
   WHERE t.volume_id=NEW.id AND t.volume_generation=NEW.generation AND t.kind='PREPARE_WORKSPACE_VOLUME'
     AND t.frozen_input->>'preparation_task_ref'=NEW.preparation_task_ref
     AND a.grant_state='CLOSED' AND a.reported_at IS NOT NULL AND a.observed_pvc_uid=NEW.pvc_uid
     AND a.job_uid IS NOT NULL AND a.pod_uid IS NOT NULL AND a.ready_record=NEW.ready_record) THEN
   RAISE EXCEPTION 'workspace volume positive preparation proof is missing';
 END IF;
 -- Отдельный протокол physical cleanup должен быть подключён до включения профиля.
 -- Обычный CRUD не может объявить удалённым том с возможными private data.
 IF NEW.physical_deleted THEN
   RAISE EXCEPTION 'workspace volume physical deletion proof is unavailable';
 END IF;
 IF NEW.ready_record IS NOT NULL THEN
   v_ready:=NEW.ready_record::jsonb;
   SELECT jsonb_build_object('schema','kodex.workspace-volume-ready.v1',
     'preparation',jsonb_build_object('scope',jsonb_build_object('organization_ref',o.ref,'project_ref',p.ref,
       'session_ref',s.ref,'agent_ref',a.ref),'workspace_ref',w.ref,'task_ref',NEW.preparation_task_ref,
       'pvc_uid',NEW.pvc_uid,'volume_generation',NEW.generation),
     'device',v_ready->'device','inode',v_ready->'inode') INTO v_expected
   FROM control_plane.session_workspace_storage w
   JOIN control_plane.sessions s ON s.id=w.session_id
   JOIN control_plane.agents a ON a.id=w.agent_id
   JOIN control_plane.projects p ON p.id=w.project_id
   JOIN control_plane.organizations o ON o.id=w.organization_id
   WHERE w.id=NEW.workspace_id;
   IF v_ready IS DISTINCT FROM v_expected OR jsonb_typeof(v_ready->'device') IS DISTINCT FROM 'number'
      OR jsonb_typeof(v_ready->'inode') IS DISTINCT FROM 'number'
      OR (v_ready->>'device' ~ '^[1-9][0-9]{0,19}$') IS NOT TRUE
      OR (v_ready->>'inode' ~ '^[1-9][0-9]{0,19}$') IS NOT TRUE
      OR (v_ready->>'device')::numeric>18446744073709551615
      OR (v_ready->>'inode')::numeric>18446744073709551615 THEN
     RAISE EXCEPTION 'workspace volume ready observation is invalid';
   END IF;
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER workspace_volume_custody_guard BEFORE INSERT OR UPDATE OR DELETE
 ON control_plane.session_workspace_volumes FOR EACH ROW EXECUTE FUNCTION control_plane.guard_workspace_volume_custody();

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_workspace_volume_task() RETURNS trigger
LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog AS $$
DECLARE v_expected jsonb;
BEGIN
 IF TG_OP='DELETE' THEN
   IF NOT control_plane.project_purge_authorized() OR OLD.state IN('READY','CLAIMED') THEN
     RAISE EXCEPTION 'workspace volume task is retained';
   END IF;
   RETURN OLD;
 END IF;
 IF TG_OP='UPDATE' THEN
   IF ROW(NEW.id,NEW.ref,NEW.volume_id,NEW.volume_generation,NEW.origin_node_id,NEW.root_run_id,
      NEW.origin_actor_id,NEW.kind,NEW.frozen_input,NEW.maximum_attempts,NEW.created_at)
      IS DISTINCT FROM ROW(OLD.id,OLD.ref,OLD.volume_id,OLD.volume_generation,OLD.origin_node_id,OLD.root_run_id,
      OLD.origin_actor_id,OLD.kind,OLD.frozen_input,OLD.maximum_attempts,OLD.created_at) THEN
     RAISE EXCEPTION 'workspace volume logical task is immutable';
   END IF;
   IF OLD.state IN('SUCCEEDED','CANCELLED','DEAD_LETTER') AND
      ROW(NEW.state,NEW.attempt,NEW.available_at,NEW.safe_error_code,NEW.completed_at) IS DISTINCT FROM
      ROW(OLD.state,OLD.attempt,OLD.available_at,OLD.safe_error_code,OLD.completed_at) THEN
     RAISE EXCEPTION 'workspace volume terminal task is immutable';
   END IF;
   IF NEW.state='SUCCEEDED' AND NOT EXISTS(
      SELECT 1 FROM control_plane.session_workspace_volume_attempts a
      WHERE a.task_id=NEW.id AND a.grant_state='CLOSED' AND a.observed_pvc_uid IS NOT NULL) THEN
     RAISE EXCEPTION 'workspace volume success has no positive physical observation';
   END IF;
   IF NEW.attempt<>OLD.attempt THEN
     IF NEW.attempt<>OLD.attempt+1 OR NEW.state<>'CLAIMED' OR OLD.state NOT IN('READY','CLAIMED') OR OLD.attempt>=5 THEN
       RAISE EXCEPTION 'workspace volume attempt transition is invalid';
     END IF;
   ELSIF NEW.state<>OLD.state AND NOT(OLD.state IN('READY','CLAIMED')
      AND NEW.state IN('SUCCEEDED','CANCELLED','DEAD_LETTER')) THEN
     RAISE EXCEPTION 'workspace volume task transition is invalid';
   END IF;
   RETURN NEW;
 END IF;
 IF NEW.state<>'READY' OR NEW.attempt<>0 THEN
   RAISE EXCEPTION 'workspace volume task initial state is invalid';
 END IF;
 SELECT jsonb_build_object('profile','LOCAL_PATH_V1','scope',jsonb_build_object('organization_ref',o.ref,
   'project_ref',p.ref,'session_ref',s.ref,'agent_ref',a.ref),'workspace_ref',w.ref,
   'volume_generation',v.generation,'create_task_ref',v.create_task_ref,'read_only',NEW.frozen_input->'read_only')
   || CASE WHEN NEW.kind='PREPARE_WORKSPACE_VOLUME' THEN jsonb_build_object(
      'preparation_task_ref',v.preparation_task_ref,'pvc_uid',v.pvc_uid) ELSE '{}'::jsonb END INTO v_expected
 FROM control_plane.session_workspace_volumes v
 JOIN control_plane.session_workspace_storage w ON w.id=v.workspace_id
 JOIN control_plane.sessions s ON s.id=w.session_id
 JOIN control_plane.agents a ON a.id=w.agent_id
 JOIN control_plane.projects p ON p.id=w.project_id
 JOIN control_plane.organizations o ON o.id=w.organization_id
 JOIN control_plane.run_nodes n ON n.id=NEW.origin_node_id AND n.organization_id=w.organization_id
   AND n.agent_id=w.agent_id AND n.type='AGENT_EXECUTION' AND n.state='QUEUED'
 JOIN control_plane.runs r ON r.id=n.run_id AND r.session_id=w.session_id AND r.project_id=w.project_id
 JOIN control_plane.runs root ON root.id=NEW.root_run_id AND root.id=r.root_run_id
   AND root.organization_id=w.organization_id AND root.project_id=w.project_id AND root.initiated_by=NEW.origin_actor_id
 JOIN control_plane.subjects actor ON actor.id=NEW.origin_actor_id AND actor.active
 WHERE v.id=NEW.volume_id AND v.generation=NEW.volume_generation AND w.state='EMPTY' AND w.writer_lease_id IS NULL
   AND p.lifecycle='ACTIVE' AND s.state='ACTIVE' AND a.enabled
   AND r.state IN('QUEUED','RUNNING') AND root.state IN('QUEUED','RUNNING','WAITING_HUMAN')
   AND jsonb_typeof(NEW.frozen_input->'read_only')='boolean'
   AND (NEW.kind='CREATE_WORKSPACE_VOLUME' AND v.state='CREATE_PENDING'
      AND v.create_task_ref=NEW.ref AND NEW.frozen_input->'read_only'='false'::jsonb
      OR NEW.kind='PREPARE_WORKSPACE_VOLUME' AND v.state='PREPARE_PENDING' AND v.pvc_uid IS NOT NULL
        AND v.preparation_task_ref IS NOT NULL AND
        (NEW.frozen_input->'read_only'='true'::jsonb OR v.preparation_task_ref=NEW.ref));
 IF v_expected IS NULL OR NEW.frozen_input IS DISTINCT FROM v_expected THEN
   RAISE EXCEPTION 'workspace volume task origin is invalid';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER workspace_volume_task_guard BEFORE INSERT OR UPDATE OR DELETE
 ON control_plane.session_workspace_volume_tasks FOR EACH ROW EXECUTE FUNCTION control_plane.guard_workspace_volume_task();

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_workspace_volume_attempt() RETURNS trigger
LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog AS $$
DECLARE v_task record; v_frame jsonb; v_expected jsonb; v_ready jsonb;
BEGIN
 IF TG_OP='DELETE' THEN
   IF NOT control_plane.project_purge_authorized() OR OLD.grant_state<>'CLOSED' THEN
     RAISE EXCEPTION 'workspace volume attempt is retained';
   END IF;
   RETURN OLD;
 END IF;
 SELECT t.state,t.attempt,t.kind,t.ref,t.frozen_input,t.volume_generation INTO STRICT v_task
 FROM control_plane.session_workspace_volume_tasks t WHERE t.id=NEW.task_id;
 IF TG_OP='INSERT' THEN
   IF v_task.state<>'CLAIMED' OR v_task.attempt<>NEW.attempt OR NEW.grant_state<>'ACTIVE'
      OR NEW.observed_pvc_uid IS NOT NULL OR NEW.job_uid IS NOT NULL OR NEW.pod_uid IS NOT NULL OR NEW.ready_record IS NOT NULL
      OR NEW.reported_at IS NOT NULL OR NEW.expires_at<=statement_timestamp()
      OR NEW.expires_at>statement_timestamp()+interval '3 minutes' THEN
     RAISE EXCEPTION 'workspace volume attempt initial grant is invalid';
   END IF;
 ELSE
   IF ROW(NEW.task_id,NEW.attempt,NEW.lease_ref,NEW.fence_digest,NEW.workload_instance,NEW.input_snapshot,
      NEW.input_digest,NEW.created_at) IS DISTINCT FROM ROW(OLD.task_id,OLD.attempt,OLD.lease_ref,
      OLD.fence_digest,OLD.workload_instance,OLD.input_snapshot,OLD.input_digest,OLD.created_at)
      OR OLD.grant_state='CLOSED' AND NEW.grant_state<>'CLOSED' THEN
     RAISE EXCEPTION 'workspace volume attempt identity is immutable';
   END IF;
   IF NEW.expires_at IS DISTINCT FROM OLD.expires_at AND (
      OLD.grant_state<>'ACTIVE' OR NEW.grant_state<>'ACTIVE' OR NEW.expires_at<OLD.expires_at
      OR OLD.expires_at<=statement_timestamp() OR NEW.expires_at>statement_timestamp()+interval '3 minutes') THEN
     RAISE EXCEPTION 'workspace volume renewal is invalid';
   END IF;
   IF OLD.reported_at IS NOT NULL AND ROW(NEW.observed_pvc_uid,NEW.job_uid,NEW.pod_uid,NEW.ready_record,NEW.reported_at)
      IS DISTINCT FROM ROW(OLD.observed_pvc_uid,OLD.job_uid,OLD.pod_uid,OLD.ready_record,OLD.reported_at) THEN
     RAISE EXCEPTION 'workspace volume positive observation is immutable';
   END IF;
 END IF;
 v_frame:=NEW.input_snapshot::jsonb;
 v_expected:=jsonb_build_object('task_ref',v_task.ref,'kind',v_task.kind,'attempt',NEW.attempt,
   'input_digest','','reconcile_only',(v_task.frozen_input->>'read_only')::boolean OR NEW.attempt>1,
   'volume',v_task.frozen_input);
 IF v_frame IS DISTINCT FROM v_expected THEN
   RAISE EXCEPTION 'workspace volume attempt input differs from its frozen intent';
 END IF;
 IF NEW.reported_at IS NOT NULL THEN
   IF NEW.grant_state<>'CLOSED' THEN
     RAISE EXCEPTION 'workspace volume observation retained an active grant';
   END IF;
   IF v_task.kind='CREATE_WORKSPACE_VOLUME' THEN
     IF NEW.job_uid IS NOT NULL OR NEW.pod_uid IS NOT NULL OR NEW.ready_record IS NOT NULL OR NEW.attempt<>1 THEN
       RAISE EXCEPTION 'workspace volume create observation is invalid';
     END IF;
   ELSE
     IF NEW.observed_pvc_uid IS DISTINCT FROM v_task.frozen_input->>'pvc_uid'
        OR NEW.job_uid IS NULL OR NEW.pod_uid IS NULL OR NEW.ready_record IS NULL THEN
       RAISE EXCEPTION 'workspace volume preparation observation is incomplete';
     END IF;
     v_ready:=NEW.ready_record::jsonb;
     v_expected:=jsonb_build_object('schema','kodex.workspace-volume-ready.v1',
       'preparation',jsonb_build_object('scope',v_task.frozen_input->'scope',
         'workspace_ref',v_task.frozen_input->>'workspace_ref','task_ref',v_task.frozen_input->>'preparation_task_ref',
         'pvc_uid',NEW.observed_pvc_uid,'volume_generation',v_task.volume_generation),
       'device',v_ready->'device','inode',v_ready->'inode');
     IF v_ready IS DISTINCT FROM v_expected OR
        jsonb_typeof(v_ready->'device') IS DISTINCT FROM 'number' OR
        jsonb_typeof(v_ready->'inode') IS DISTINCT FROM 'number' OR
        (v_ready->>'device' ~ '^[1-9][0-9]{0,19}$') IS NOT TRUE OR
        (v_ready->>'inode' ~ '^[1-9][0-9]{0,19}$') IS NOT TRUE OR
        (v_ready->>'device')::numeric>18446744073709551615 OR
        (v_ready->>'inode')::numeric>18446744073709551615 THEN
       RAISE EXCEPTION 'workspace volume preparation observation differs from its input';
     END IF;
   END IF;
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER workspace_volume_attempt_guard BEFORE INSERT OR UPDATE OR DELETE
 ON control_plane.session_workspace_volume_attempts FOR EACH ROW EXECUTE FUNCTION control_plane.guard_workspace_volume_attempt();

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_workspace_volume_envelope() RETURNS trigger
LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog AS $$
DECLARE v_id uuid; v_task record; v_count integer; v_active integer;
BEGIN
 IF TG_TABLE_NAME='session_workspace_volume_tasks' THEN
   IF TG_OP='DELETE' THEN v_id:=OLD.id; ELSE v_id:=NEW.id; END IF;
 ELSE
   IF TG_OP='DELETE' THEN v_id:=OLD.task_id; ELSE v_id:=NEW.task_id; END IF;
 END IF;
 SELECT t.state,t.attempt INTO v_task FROM control_plane.session_workspace_volume_tasks t WHERE t.id=v_id;
 IF NOT FOUND THEN
   -- BEFORE DELETE guards и deferred FK уже удержали точную purge authority; transaction-local flag не является durable receipt.
   RETURN NULL;
 END IF;
 SELECT count(*),count(*) FILTER(WHERE a.grant_state='ACTIVE') INTO v_count,v_active
 FROM control_plane.session_workspace_volume_attempts a WHERE a.task_id=v_id;
 IF v_count<>v_task.attempt OR (v_task.state='CLAIMED' AND v_active<>1)
    OR (v_task.state<>'CLAIMED' AND v_active<>0)
    OR EXISTS(SELECT 1 FROM control_plane.session_workspace_volume_attempts a
      WHERE a.task_id=v_id AND (a.attempt>v_task.attempt OR a.grant_state='ACTIVE' AND a.attempt<>v_task.attempt)) THEN
   RAISE EXCEPTION 'workspace volume task envelope is partial';
 END IF;
 RETURN NULL;
END;
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER workspace_volume_task_envelope
 AFTER INSERT OR UPDATE OR DELETE ON control_plane.session_workspace_volume_tasks
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION control_plane.guard_workspace_volume_envelope();
CREATE CONSTRAINT TRIGGER workspace_volume_attempt_envelope
 AFTER INSERT OR UPDATE OR DELETE ON control_plane.session_workspace_volume_attempts
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION control_plane.guard_workspace_volume_envelope();

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_workspace_volume_lineage() RETURNS trigger
LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog AS $$
DECLARE v_volume record;
BEGIN
 SELECT v.id,v.generation,v.create_task_ref,v.preparation_task_ref INTO v_volume
 FROM control_plane.session_workspace_volumes v WHERE v.id=NEW.id;
 IF NOT FOUND THEN RETURN NULL; END IF;
 IF NOT EXISTS(SELECT 1 FROM control_plane.session_workspace_volume_tasks t
      WHERE t.ref=v_volume.create_task_ref AND t.volume_id=v_volume.id
        AND t.volume_generation=v_volume.generation AND t.kind='CREATE_WORKSPACE_VOLUME')
    OR v_volume.preparation_task_ref IS NOT NULL AND NOT EXISTS(
      SELECT 1 FROM control_plane.session_workspace_volume_tasks t
      WHERE t.ref=v_volume.preparation_task_ref AND t.volume_id=v_volume.id
        AND t.volume_generation=v_volume.generation AND t.kind='PREPARE_WORKSPACE_VOLUME'
        AND t.frozen_input->>'preparation_task_ref'=t.ref AND t.frozen_input->'read_only'='false'::jsonb) THEN
   RAISE EXCEPTION 'workspace volume task lineage is invalid';
 END IF;
 RETURN NULL;
END;
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER workspace_volume_lineage
 AFTER INSERT OR UPDATE ON control_plane.session_workspace_volumes
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION control_plane.guard_workspace_volume_lineage();

-- +goose StatementBegin
CREATE FUNCTION control_plane.close_workspace_volume_origin() RETURNS trigger
LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog AS $$
DECLARE v_task record;
BEGIN
 -- Родительская owner-команда уже удерживает изменяемую строку origin.
 -- Выборка не расширяется на другие actor/project/session этого organization.
 FOR v_task IN
   SELECT t.id,t.volume_id,t.volume_generation
   FROM control_plane.session_workspace_volume_tasks t
   JOIN control_plane.session_workspace_volumes v ON v.id=t.volume_id
   JOIN control_plane.session_workspace_storage w ON w.id=v.workspace_id
   JOIN control_plane.run_nodes n ON n.id=t.origin_node_id
   WHERE t.state IN('READY','CLAIMED') AND (
     TG_TABLE_NAME='runs' AND (t.root_run_id=NEW.id OR n.run_id=NEW.id)
     OR TG_TABLE_NAME='run_nodes' AND t.origin_node_id=NEW.id
     OR TG_TABLE_NAME='sessions' AND w.session_id=NEW.id
     OR TG_TABLE_NAME='agents' AND w.agent_id=NEW.id
     OR TG_TABLE_NAME='projects' AND w.project_id=NEW.id
     OR TG_TABLE_NAME='subjects' AND t.origin_actor_id=NEW.id)
   ORDER BY t.root_run_id,w.session_id,v.id,t.id FOR UPDATE OF v,t
 LOOP
   UPDATE control_plane.session_workspace_volume_attempts SET grant_state='CLOSED'
   WHERE task_id=v_task.id AND grant_state='ACTIVE';
   UPDATE control_plane.session_workspace_volume_tasks
   SET state='CANCELLED',safe_error_code='ORIGIN_CLOSED',completed_at=clock_timestamp(),updated_at=clock_timestamp()
   WHERE id=v_task.id AND state IN('READY','CLAIMED');
   UPDATE control_plane.session_workspace_volumes
   SET state='BLOCKED',block_reason='ORIGIN_CLOSED',version=version+1,updated_at=clock_timestamp()
   WHERE id=v_task.volume_id AND generation=v_task.volume_generation AND state NOT IN('READY','PURGED');
 END LOOP;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER workspace_volume_root_closed AFTER UPDATE OF state ON control_plane.runs
 FOR EACH ROW WHEN(OLD.state IS DISTINCT FROM NEW.state AND NEW.state IN('SUCCEEDED','FAILED','CANCELLED'))
 EXECUTE FUNCTION control_plane.close_workspace_volume_origin();
CREATE TRIGGER workspace_volume_node_closed AFTER UPDATE OF state ON control_plane.run_nodes
 FOR EACH ROW WHEN(OLD.state IS DISTINCT FROM NEW.state AND NEW.state<>'QUEUED')
 EXECUTE FUNCTION control_plane.close_workspace_volume_origin();
CREATE TRIGGER workspace_volume_session_closed AFTER UPDATE OF state ON control_plane.sessions
 FOR EACH ROW WHEN(OLD.state IS DISTINCT FROM NEW.state AND NEW.state<>'ACTIVE')
 EXECUTE FUNCTION control_plane.close_workspace_volume_origin();
CREATE TRIGGER workspace_volume_agent_closed AFTER UPDATE OF enabled ON control_plane.agents
 FOR EACH ROW WHEN(OLD.enabled AND NOT NEW.enabled)
 EXECUTE FUNCTION control_plane.close_workspace_volume_origin();
CREATE TRIGGER workspace_volume_project_closed AFTER UPDATE OF lifecycle ON control_plane.projects
 FOR EACH ROW WHEN(OLD.lifecycle IS DISTINCT FROM NEW.lifecycle AND NEW.lifecycle<>'ACTIVE')
 EXECUTE FUNCTION control_plane.close_workspace_volume_origin();
CREATE TRIGGER workspace_volume_actor_closed AFTER UPDATE OF active ON control_plane.subjects
 FOR EACH ROW WHEN(OLD.active AND NOT NEW.active)
 EXECUTE FUNCTION control_plane.close_workspace_volume_origin();

REVOKE ALL ON control_plane.session_workspace_volumes,control_plane.session_workspace_volume_tasks,
 control_plane.session_workspace_volume_attempts FROM PUBLIC;
GRANT SELECT,INSERT,UPDATE,DELETE ON control_plane.session_workspace_volumes,control_plane.session_workspace_volume_tasks,
 control_plane.session_workspace_volume_attempts TO control_plane_runtime;
REVOKE ALL ON FUNCTION control_plane.guard_workspace_volume_custody(),control_plane.guard_workspace_volume_task(),
 control_plane.guard_workspace_volume_attempt(),control_plane.guard_workspace_volume_envelope(),
 control_plane.guard_workspace_volume_lineage(),control_plane.close_workspace_volume_origin() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.guard_workspace_volume_custody(),control_plane.guard_workspace_volume_task(),
 control_plane.guard_workspace_volume_attempt(),control_plane.guard_workspace_volume_envelope(),
 control_plane.guard_workspace_volume_lineage(),control_plane.close_workspace_volume_origin() TO control_plane_runtime;

-- +goose StatementBegin
DO $purge$
DECLARE v_definition text;v_source text;v_expected_source text;v_old_edges text;v_old_nodes text;
 v_nodes oid[];v_old oid[];v_added oid[];v_count integer;v_extra integer;v_digest text;v_new_edges text;v_new_nodes text;
 v_function oid:='control_plane.purge_project_database(uuid,uuid,text)'::regprocedure;v_links jsonb;
BEGIN
 SELECT pg_get_functiondef(p.oid),p.prosrc INTO STRICT v_definition,v_source FROM pg_proc p
 WHERE p.oid=v_function AND p.proowner='control_plane_owner'::regrole AND p.prosecdef AND p.provolatile='v'
 AND p.prorettype='integer'::regtype AND NOT p.proretset AND p.proconfig=ARRAY['search_path=pg_catalog']
 AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql')
 AND has_function_privilege('control_plane_runtime',p.oid,'EXECUTE')
 AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a
   WHERE a.grantee NOT IN('control_plane_owner'::regrole,'control_plane_runtime'::regrole)
     OR a.grantee='control_plane_runtime'::regrole AND (a.is_grantable OR a.privilege_type<>'EXECUTE'));
 v_old_edges:=substring(v_source FROM 'IF v_edges <> 296 OR v_edge_fingerprint <> ''[a-f0-9]{32}'' THEN');
 v_old_nodes:=substring(v_source FROM 'IF v_nodes <> 106 OR v_node_fingerprint <> ''[a-f0-9]{32}'' THEN');
 IF v_old_edges IS NULL OR v_old_nodes IS NULL OR md5(replace(replace(v_source,v_old_edges,
   'IF v_edges <> 284 OR v_edge_fingerprint <> ''6bf6ad76d8c7ae66e07f75ecf719fdd6'' THEN'),v_old_nodes,
   'IF v_nodes <> 105 OR v_node_fingerprint <> ''f51a0de25bd942a250997775617eeaee'' THEN'))<>'b886c24d4e2f5c4a0ec08123d99462b0' THEN
   RAISE EXCEPTION 'workspace volume purge predecessor changed';
 END IF;
 v_added:=ARRAY['control_plane.session_workspace_volumes'::regclass,
   'control_plane.session_workspace_volume_tasks'::regclass,'control_plane.session_workspace_volume_attempts'::regclass]::oid[];
 WITH RECURSIVE nodes(id) AS(SELECT 'control_plane.projects'::regclass::oid UNION
   SELECT c.conrelid FROM pg_constraint c JOIN nodes n ON c.confrelid=n.id
   WHERE c.contype='f' AND c.connamespace='control_plane'::regnamespace)
 SELECT array_agg(id),array_agg(id) FILTER(WHERE NOT(id=ANY(v_added))) INTO v_nodes,v_old FROM nodes;
 SELECT count(*),md5(string_agg(n.nspname||'.'||r.relname,E'\n' ORDER BY n.nspname,r.relname)) INTO v_count,v_digest
 FROM pg_class r JOIN pg_namespace n ON n.oid=r.relnamespace WHERE r.oid=ANY(v_old);
 IF v_count<>106 OR v_old_nodes IS DISTINCT FROM format('IF v_nodes <> %s OR v_node_fingerprint <> %L THEN',v_count,v_digest) THEN
   RAISE EXCEPTION 'workspace volume predecessor nodes changed';
 END IF;
 SELECT count(*),md5(string_agg(cn.nspname||'.'||cc.relname||'|'||c.conname||'|'||pn.nspname||'.'||pc.relname||'|'||c.confdeltype::text,E'\n'
   ORDER BY cn.nspname,cc.relname,c.conname)) INTO v_count,v_digest
 FROM pg_constraint c JOIN pg_class cc ON cc.oid=c.conrelid JOIN pg_namespace cn ON cn.oid=cc.relnamespace
 JOIN pg_class pc ON pc.oid=c.confrelid JOIN pg_namespace pn ON pn.oid=pc.relnamespace
 WHERE c.contype='f' AND c.conrelid=ANY(v_old) AND c.confrelid=ANY(v_old);
 IF v_count<>296 OR v_old_edges IS DISTINCT FROM format('IF v_edges <> %s OR v_edge_fingerprint <> %L THEN',v_count,v_digest) THEN
   RAISE EXCEPTION 'workspace volume predecessor edges changed';
 END IF;
 SELECT jsonb_agg(jsonb_build_array(c.conrelid::regclass::text,c.conname,c.confrelid::regclass::text,
   c.confdeltype::text,c.condeferrable,c.condeferred,c.convalidated,c.conenforced)
   ORDER BY c.conrelid::regclass::text,c.conname) INTO v_links
 FROM pg_constraint c WHERE c.contype='f' AND (c.conrelid=ANY(v_added) OR c.confrelid=ANY(v_added));
 IF v_links IS DISTINCT FROM '[
 ["control_plane.session_workspace_volume_attempts","session_workspace_volume_attempts_task_id_fkey","control_plane.session_workspace_volume_tasks","a",true,false,true,true],
 ["control_plane.session_workspace_volume_tasks","session_workspace_volume_tasks_origin_actor_id_fkey","control_plane.subjects","a",true,false,true,true],
 ["control_plane.session_workspace_volume_tasks","session_workspace_volume_tasks_origin_node_id_fkey","control_plane.run_nodes","a",true,false,true,true],
 ["control_plane.session_workspace_volume_tasks","session_workspace_volume_tasks_root_run_id_fkey","control_plane.runs","a",true,false,true,true],
 ["control_plane.session_workspace_volume_tasks","session_workspace_volume_tasks_volume_id_fkey","control_plane.session_workspace_volumes","a",true,false,true,true],
 ["control_plane.session_workspace_volumes","session_workspace_volume_create_task","control_plane.session_workspace_volume_tasks","a",true,true,true,true],
 ["control_plane.session_workspace_volumes","session_workspace_volume_prepare_task","control_plane.session_workspace_volume_tasks","a",true,true,true,true],
 ["control_plane.session_workspace_volumes","session_workspace_volumes_workspace_id_fkey","control_plane.session_workspace_storage","a",true,false,true,true]]'::jsonb THEN
   RAISE EXCEPTION 'workspace volume graph delta changed';
 END IF;
 IF cardinality(v_nodes)<>109 OR cardinality(v_old)<>106 THEN
   RAISE EXCEPTION 'workspace volume graph nodes changed';
 END IF;
 SELECT count(*) INTO v_extra FROM pg_constraint c WHERE c.contype='f' AND c.conrelid=ANY(v_nodes) AND c.confrelid=ANY(v_nodes)
   AND (c.conrelid=ANY(v_added) OR c.confrelid=ANY(v_added));
 SELECT count(*),md5(string_agg(cn.nspname||'.'||cc.relname||'|'||c.conname||'|'||pn.nspname||'.'||pc.relname||'|'||c.confdeltype::text,E'\n'
   ORDER BY cn.nspname,cc.relname,c.conname)) INTO v_count,v_digest
 FROM pg_constraint c JOIN pg_class cc ON cc.oid=c.conrelid JOIN pg_namespace cn ON cn.oid=cc.relnamespace
 JOIN pg_class pc ON pc.oid=c.confrelid JOIN pg_namespace pn ON pn.oid=pc.relnamespace
 WHERE c.contype='f' AND c.conrelid=ANY(v_nodes) AND c.confrelid=ANY(v_nodes);
 IF v_count<>296+v_extra OR v_extra<7 OR v_extra>8 THEN
   RAISE EXCEPTION 'workspace volume graph edge count changed';
 END IF;
 v_new_edges:=format('IF v_edges <> %s OR v_edge_fingerprint <> %L THEN',v_count,v_digest);
 SELECT md5(string_agg(n.nspname||'.'||r.relname,E'\n' ORDER BY n.nspname,r.relname)) INTO v_digest
 FROM pg_class r JOIN pg_namespace n ON n.oid=r.relnamespace WHERE r.oid=ANY(v_nodes);
 v_new_nodes:=format('IF v_nodes <> 109 OR v_node_fingerprint <> %L THEN',v_digest);
 v_expected_source:=replace(replace(v_source,v_old_edges,v_new_edges),v_old_nodes,v_new_nodes);
 EXECUTE replace(replace(v_definition,v_old_edges,v_new_edges),v_old_nodes,v_new_nodes);
 IF NOT EXISTS(SELECT 1 FROM pg_proc p WHERE p.oid=v_function AND p.prosrc=v_expected_source) THEN
   RAISE EXCEPTION 'workspace volume purge function readback failed';
 END IF;
END $purge$;
-- +goose StatementEnd
RESET ROLE;

-- +goose Down
-- Custody и ledger не удаляются обычным rollback после возможного внешнего действия.
-- +goose StatementBegin
DO $forward_only$
BEGIN
 RAISE EXCEPTION 'workspace volume custody requires forward repair';
END $forward_only$;
-- +goose StatementEnd
