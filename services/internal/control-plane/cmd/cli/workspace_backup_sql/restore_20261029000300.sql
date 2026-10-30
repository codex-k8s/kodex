-- Generated: generate-workflow-pdlc-proof.py; exact backup restore031.
-- Generated: generate-github-pr-proof.py; только мигратор, без пользовательских payload.
SET LOCAL ROLE control_plane_owner;
SET LOCAL lock_timeout='8s';
SET LOCAL statement_timeout='30s';
SELECT pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended('kodex.workspace.backup-acl-maintenance.v1',0));
DO $backup_boundary$
BEGIN
 IF current_database()<>'control_plane' OR session_user<>'control_plane_migrator'
 OR NOT EXISTS(SELECT 1 FROM pg_catalog.pg_roles WHERE rolname='kodex_backup_reader'
  AND rolcanlogin AND NOT rolsuper AND NOT rolcreatedb AND NOT rolcreaterole AND NOT rolinherit AND NOT rolreplication AND rolbypassrls)
 OR EXISTS(SELECT 1 FROM pg_catalog.pg_auth_members m JOIN pg_catalog.pg_roles r ON r.oid=m.member WHERE r.rolname='kodex_backup_reader')
 OR EXISTS(SELECT 1 FROM control_plane.runs WHERE state IN('QUEUED','RUNNING','CANCELLING','WAITING_HUMAN'))
 OR EXISTS(SELECT 1 FROM control_plane.run_nodes WHERE state IN('QUEUED','RUNNING','WAITING'))
 OR EXISTS(SELECT 1 FROM control_plane.session_turns WHERE state IN('QUEUED','RUNNING'))
 OR EXISTS(SELECT 1 FROM control_plane.runtime_leases WHERE state='CLAIMED')
 OR EXISTS(SELECT 1 FROM control_plane.session_archive_tasks WHERE state IN('READY','CLAIMED'))
 OR EXISTS(SELECT 1 FROM control_plane.session_workspace_storage) THEN
  RAISE EXCEPTION 'workspace backup maintenance boundary is not idle';
 END IF;
END $backup_boundary$;
DO $backup_version$ BEGIN IF (SELECT max(version_id) FROM public.goose_db_version WHERE is_applied) IS DISTINCT FROM 20261029000300::bigint THEN RAISE EXCEPTION 'workspace backup restore version changed'; END IF; END $backup_version$;
DO $backup_acl$
DECLARE target text; relation oid;
BEGIN
 FOREACH target IN ARRAY ARRAY['session_workspace_storage','session_archives','session_archive_tasks','session_workspace_volumes','session_workspace_volume_tasks','session_workspace_volume_attempts'] LOOP
  relation:=pg_catalog.to_regclass('control_plane.'||target);
  IF relation IS NULL THEN CONTINUE; END IF;
  IF EXISTS(SELECT 1 FROM pg_catalog.pg_class c WHERE c.oid=relation AND
    (c.relkind<>'r' OR pg_catalog.pg_get_userbyid(c.relowner)<>'control_plane_owner'))
   OR EXISTS(SELECT 1 FROM pg_catalog.pg_class c CROSS JOIN LATERAL pg_catalog.aclexplode(c.relacl) a
    WHERE c.oid=relation AND pg_catalog.pg_get_userbyid(a.grantee)='kodex_backup_reader'
      AND (a.privilege_type<>'SELECT' OR a.is_grantable OR pg_catalog.pg_get_userbyid(a.grantor)<>'control_plane_owner'))
   OR EXISTS(SELECT 1 FROM pg_catalog.pg_attribute a WHERE a.attrelid=relation AND a.attnum>0 AND a.attacl IS NOT NULL) THEN
   RAISE EXCEPTION 'workspace backup table authority changed';
  END IF;
 END LOOP;
 IF EXISTS(SELECT 1 FROM pg_catalog.pg_default_acl d CROSS JOIN LATERAL pg_catalog.aclexplode(d.defaclacl) a
  WHERE pg_catalog.pg_get_userbyid(d.defaclrole)='control_plane_owner' AND d.defaclobjtype='r'
   AND pg_catalog.pg_get_userbyid(a.grantee)='kodex_backup_reader'
   AND (d.defaclnamespace<>'control_plane'::regnamespace OR a.privilege_type<>'SELECT' OR a.is_grantable
        OR pg_catalog.pg_get_userbyid(a.grantor)<>'control_plane_owner')) THEN
  RAISE EXCEPTION 'workspace backup default authority changed';
 END IF;
END $backup_acl$;
REVOKE SELECT ON control_plane.session_workspace_storage,control_plane.session_archives,control_plane.session_archive_tasks,control_plane.session_workspace_volumes,control_plane.session_workspace_volume_tasks,control_plane.session_workspace_volume_attempts FROM kodex_backup_reader RESTRICT;
-- Проверка actual restore extension до любой inverse projection.
DO $restore_catalog$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_catalog.pg_proc p
  WHERE p.oid='control_plane.guard_workspace_archive_task()'::regprocedure
   AND md5(p.prosrc)='93a6276029ab000b40c090d846748cad')
 OR NOT EXISTS(SELECT 1 FROM pg_catalog.pg_index i JOIN pg_catalog.pg_class c ON c.oid=i.indexrelid
   WHERE c.relname='session_archive_tasks_workspace_restore' AND i.indrelid='control_plane.session_archive_tasks'::regclass
    AND i.indisunique AND i.indisvalid AND i.indisready AND i.indislive
    AND pg_catalog.pg_get_indexdef(i.indexrelid)=$index$CREATE UNIQUE INDEX session_archive_tasks_workspace_restore ON control_plane.session_archive_tasks USING btree (workspace_id, workspace_generation) WHERE ((workspace_id IS NOT NULL) AND (kind = 'RESTORE'::text))$index$)
 OR NOT EXISTS(SELECT 1 FROM pg_catalog.pg_constraint c WHERE c.conrelid='control_plane.session_workspace_storage'::regclass
   AND c.conname='session_workspace_storage_block_reason_check' AND c.contype='c' AND c.convalidated AND c.conenforced
   AND pg_catalog.pg_get_constraintdef(c.oid)=$check$CHECK ((block_reason = ANY (ARRAY[''::text, 'POD_MISSING'::text, 'POD_UNREACHABLE'::text, 'PVC_MISSING'::text, 'PVC_REPLACED'::text, 'OWNERSHIP_CHANGED'::text, 'WRITER_JOIN_UNCONFIRMED'::text, 'RESTORE_UNCONFIRMED'::text])))$check$) THEN
  RAISE EXCEPTION 'workspace restore catalog changed';
 END IF;
END $restore_catalog$;

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

-- Actual SSH extension проверяется целиком до обратной014 проекции.
DO $combined_effect$
DECLARE v_source text; v_extension text:=$combined_deployment$                    OR (requirement.value->>'Kind'='DEPLOYMENT' AND requirement.value->>'Operation'='ssh.compose.deploy'
                        AND invocation.operation='ssh.compose.deploy' AND invocation.resource_kind='SSH_TARGET' AND connection.definition_key='ssh-compose'
                        AND invocation.resource_scope->>'target_ref'=requirement.value->>'TargetRef'
                        AND invocation.ssh_compose_snapshot->'target'->>'ref'=requirement.value->>'TargetRef'
                        AND invocation.ssh_compose_snapshot->>'originRuntimeRevisionId'=revision.id::text
                        AND invocation.ssh_compose_snapshot->>'originLeaseId'=lease.id::text
                        AND invocation.ssh_compose_snapshot->'originGeneration'=to_jsonb(revision.generation)
                        AND invocation.ssh_compose_snapshot->>'deploymentRef'=invocation.ref
                        AND invocation.ssh_compose_snapshot->>'deploymentInputDigest'=invocation.input_digest
                        AND invocation.ssh_compose_snapshot->>'deploymentEffectKey'=invocation.effect_key
                        AND EXISTS (
                            SELECT 1 FROM jsonb_array_elements(origin.workflow_inputs) dependency
                            JOIN control_plane.run_nodes source ON source.id::text=dependency->>'sourceNodeId'
                                AND source.organization_id=origin.organization_id AND source.root_run_id=origin.root_run_id
                                AND source.workflow_step_origin_node_id IS NULL AND source.workflow_business_attempt=1
                                AND source.workflow_step_key=dependency->>'stepKey'
                            CROSS JOIN LATERAL jsonb_array_elements(source.workflow_attempts) accepted
                            CROSS JOIN LATERAL jsonb_array_elements(accepted->'results') result
                            JOIN control_plane.integration_effect_receipts merge_receipt ON merge_receipt.ref=result->>'receiptRef'
                                AND merge_receipt.organization_id=origin.organization_id
                            JOIN control_plane.integration_invocations merge_invocation ON merge_invocation.id=merge_receipt.invocation_id
                                AND merge_invocation.organization_id=origin.organization_id AND merge_invocation.state='SUCCEEDED'
                                AND merge_invocation.operation='github.pull_request.merge'
                                AND merge_invocation.effect_receipt_id=merge_receipt.id AND merge_invocation.effect_key=merge_receipt.effect_key
                                AND merge_invocation.input_digest=merge_receipt.input_digest
                            WHERE dependency->>'stepKey'=requirement.value->>'SubjectStepKey'
                                AND step.value->'DependsOn' ? (dependency->>'stepKey')
                                AND accepted=dependency->'receipt' AND accepted->>'accepted'='true'
                                AND result->>'kind'='INTEGRATION_RECEIPT' AND result->>'repository'=requirement.value->>'Repository'
                                AND result->>'headSha'=invocation.ssh_compose_snapshot->>'mergedSha'
                                AND merge_invocation.ref=invocation.ssh_compose_snapshot->>'mergeInvocationRef'

                        ))
                    $combined_deployment$;
BEGIN
 SELECT prosrc INTO STRICT v_source FROM pg_catalog.pg_proc
 WHERE oid='control_plane.guard_workflow_effect_records()'::regprocedure;
 IF (length(v_source)-length(replace(v_source,v_extension,'')))/length(v_extension)<>1
 OR md5(v_source)<>'cc1954201904c1193238a5fbacf51c6f' THEN
 RAISE EXCEPTION 'combined SSH evidence guard changed';
 END IF;
END $combined_effect$;

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
   FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid WHERE i.indrelid=r.oid AND c.relname<>'session_archive_tasks_workspace_restore'),
 'triggers',(SELECT jsonb_agg(jsonb_build_array(CASE WHEN t.tgisinternal THEN regexp_replace(t.tgname,'_[0-9]+$','_<oid>') ELSE t.tgname END,
   t.tgtype,t.tgenabled,t.tgisinternal,t.tgdeferrable,t.tginitdeferred,t.tgnargs,t.tgattr::text,pg_get_expr(t.tgqual,t.tgrelid),t.tgfoid::regprocedure::text,
   c.conname,c.conrelid::regclass::text,c.confrelid::regclass::text,t.tgparentid=0,encode(t.tgargs,'hex'),t.tgoldtable,t.tgnewtable)
   ORDER BY t.tgisinternal,coalesce(c.conrelid::regclass::text,''),coalesce(c.conname,''),t.tgfoid::regprocedure::text,t.tgtype)
   FROM pg_trigger t LEFT JOIN pg_constraint c ON c.oid=t.tgconstraint WHERE t.tgrelid=r.oid),
 'functions',(SELECT jsonb_agg(jsonb_build_array(p.oid::regprocedure::text,CASE WHEN p.oid='control_plane.guard_workspace_archive_task()'::regprocedure THEN '2198d3bff772de11aade05248500e0f1' ELSE md5(p.prosrc) END,pg_get_userbyid(p.proowner),p.prosecdef,p.provolatile,p.proconfig,p.prorettype::regtype::text,l.lanname,
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
'constraints',(SELECT jsonb_agg(jsonb_build_array(c.conname,c.contype,c.convalidated,c.conenforced,c.condeferrable,c.condeferred,CASE WHEN c.conname='session_workspace_storage_block_reason_check' THEN replace(pg_get_constraintdef(c.oid),$literal$, 'RESTORE_UNCONFIRMED'::text$literal$,'') ELSE pg_get_constraintdef(c.oid) END) ORDER BY c.conname) FROM pg_constraint c WHERE c.conrelid='control_plane.session_workspace_storage'::regclass),
'indexes',(SELECT jsonb_agg(jsonb_build_array(c.relname,i.indisunique,i.indisvalid,i.indisready,pg_get_indexdef(i.indexrelid)) ORDER BY c.relname) FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid WHERE i.indrelid='control_plane.session_workspace_storage'::regclass),
'triggers',(SELECT jsonb_agg(jsonb_build_array(t.tgname,t.tgtype,t.tgenabled,t.tgisinternal,t.tgdeferrable,t.tginitdeferred,t.tgnargs,t.tgattr::text,pg_get_expr(t.tgqual,t.tgrelid),t.tgfoid::regprocedure::text) ORDER BY t.tgname) FROM pg_trigger t WHERE t.tgrelid='control_plane.session_workspace_storage'::regclass AND NOT t.tgisinternal),
'function',(SELECT jsonb_build_array(CASE WHEN p.oid='control_plane.guard_workspace_archive_task()'::regprocedure THEN '2198d3bff772de11aade05248500e0f1' ELSE md5(p.prosrc) END,pg_get_userbyid(p.proowner),p.prosecdef,p.provolatile,p.proconfig,p.prorettype::regtype::text,l.lanname) FROM pg_proc p JOIN pg_language l ON l.oid=p.prolang WHERE p.oid='control_plane.guard_session_workspace_storage()'::regprocedure)) INTO v_catalog;
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

-- Actual031 проверяется до исторической проекции, в том же snapshot.
DO $workflow_pdlc_catalog$
DECLARE v_actual jsonb;
BEGIN
-- Только schema metadata: без бизнес-строк, credential values и runtime payload.
WITH function_names(name) AS (VALUES
 ('guard_workflow_effect_records'),('guard_workflow_attempt_journal'),
 ('workflow_evidence_intent_valid'),('workflow_evidence_retains_revision'),
 ('workflow_evidence_outcome_valid'),('workflow_evidence_execution_assigned'),
 ('workflow_pdlc_file_key'),('workflow_pdlc_file_desired_digest'),('workflow_pdlc_file_epoch_digest'),
 ('workflow_pdlc_result_digest'),('workflow_evidence_delivery_operation'),('workflow_pdlc_read_order'),
 ('workflow_pdlc_author_slot'),('workflow_pdlc_intent_valid'),('workflow_receipt_effect_proof'),
 ('workflow_receipt_lineage_proof'),('workflow_native_receipt_proof'),('workflow_pdlc_receipt_fact'),
 ('workflow_pdlc_result_valid'),('workflow_pdlc_outcome_valid')
), functions AS (
 SELECT p.*,l.lanname FROM pg_catalog.pg_proc p
 JOIN pg_catalog.pg_language l ON l.oid=p.prolang
 WHERE p.pronamespace='control_plane'::regnamespace AND p.proname IN(SELECT name FROM function_names)
), function_catalog AS (
 SELECT 'control_plane.'||quote_ident(p.proname)||'('||array_to_string(ARRAY(SELECT format_type(t,NULL) FROM unnest(p.proargtypes) t),',')||')' AS signature,jsonb_build_object(
  'sourceMD5',md5(p.prosrc),'sourceSHA256',encode(sha256(convert_to(p.prosrc,'UTF8')),'hex'),
  'owner',pg_get_userbyid(p.proowner),'language',p.lanname,'kind',p.prokind,
  'securityDefiner',p.prosecdef,'volatility',p.provolatile,'parallel',p.proparallel,
  'strict',p.proisstrict,'leakproof',p.proleakproof,'set',p.proretset,
  'result',format_type(p.prorettype,NULL),'resultDefinition',pg_get_function_result(p.oid),
  'inputTypes',ARRAY(SELECT format_type(t,NULL) FROM unnest(p.proargtypes) t),
  'allTypes',CASE WHEN p.proallargtypes IS NULL THEN NULL ELSE ARRAY(SELECT format_type(t,NULL) FROM unnest(p.proallargtypes) t) END,
  'argumentNames',p.proargnames,'argumentModes',p.proargmodes,
  'defaults',p.pronargdefaults,'defaultExpression',pg_get_expr(p.proargdefaults,0),
  'variadic',p.provariadic::regtype::text,'support',p.prosupport::text,'config',p.proconfig,
  'acl',(SELECT jsonb_agg(jsonb_build_array(CASE WHEN a.grantee=0 THEN 'PUBLIC' ELSE pg_get_userbyid(a.grantee) END,
    pg_get_userbyid(a.grantor),a.privilege_type,a.is_grantable)
    ORDER BY a.grantee=0,pg_get_userbyid(a.grantee),pg_get_userbyid(a.grantor),a.privilege_type,a.is_grantable)
    FROM aclexplode(COALESCE(p.proacl,acldefault('f',p.proowner))) a)
 ) AS metadata FROM functions p
), trigger_catalog AS (
 SELECT n.nspname||'.'||r.relname||'.'||t.tgname AS name,jsonb_build_array(
  'control_plane.'||quote_ident(p.proname)||'()',t.tgtype,t.tgenabled,t.tgisinternal,t.tgdeferrable,t.tginitdeferred,
  t.tgnargs,t.tgattr::text,pg_get_expr(t.tgqual,t.tgrelid),encode(t.tgargs,'hex'),
  t.tgoldtable,t.tgnewtable,t.tgparentid<>0,t.tgconstraint<>0) AS metadata
 FROM pg_trigger t JOIN pg_class r ON r.oid=t.tgrelid JOIN pg_namespace n ON n.oid=r.relnamespace
 JOIN functions p ON p.oid=t.tgfoid
), constraint_catalog AS (
 SELECT jsonb_build_array(c.contype,c.convalidated,c.conenforced,c.conislocal,c.connoinherit,c.condeferrable,
  c.condeferred,c.coninhcount,pg_get_constraintdef(c.oid)) AS metadata
 FROM pg_constraint c WHERE c.conrelid='control_plane.run_nodes'::regclass AND c.conname='workflow_effect_record_bounds'
)
SELECT jsonb_build_object(
 'functions',(SELECT jsonb_object_agg(signature,metadata ORDER BY signature) FROM function_catalog),
 'functionCount',(SELECT count(*) FROM functions),
 'triggers',(SELECT jsonb_object_agg(name,metadata ORDER BY name) FROM trigger_catalog),
 'effectBounds',(SELECT metadata FROM constraint_catalog)
) INTO v_actual;
 IF v_actual IS DISTINCT FROM $catalog${"effectBounds":["c",true,true,true,false,false,false,0,"CHECK (((jsonb_array_length(workflow_effect_intents) <= 64) AND (jsonb_array_length(workflow_effect_rejoins) <= 64)))"],"functionCount":20,"functions":{"control_plane.guard_workflow_attempt_journal()":{"acl":[["control_plane_owner","control_plane_owner","EXECUTE",false],["control_plane_runtime","control_plane_owner","EXECUTE",false]],"allTypes":null,"argumentModes":null,"argumentNames":null,"config":["search_path=pg_catalog, control_plane"],"defaultExpression":null,"defaults":0,"inputTypes":[],"kind":"f","language":"plpgsql","leakproof":false,"owner":"control_plane_owner","parallel":"u","result":"trigger","resultDefinition":"trigger","securityDefiner":false,"set":false,"sourceMD5":"305ca9b25f0d0fa31f59abb0b7b1d7fa","sourceSHA256":"2cf89c7a8e11054480095b07c2e31c69c6ab502b942134c3dc224e1e909bbe08","strict":false,"support":"-","variadic":"-","volatility":"v"},"control_plane.guard_workflow_effect_records()":{"acl":[["control_plane_owner","control_plane_owner","EXECUTE",false],["control_plane_runtime","control_plane_owner","EXECUTE",false]],"allTypes":null,"argumentModes":null,"argumentNames":null,"config":["search_path=pg_catalog, control_plane"],"defaultExpression":null,"defaults":0,"inputTypes":[],"kind":"f","language":"plpgsql","leakproof":false,"owner":"control_plane_owner","parallel":"u","result":"trigger","resultDefinition":"trigger","securityDefiner":false,"set":false,"sourceMD5":"cc1954201904c1193238a5fbacf51c6f","sourceSHA256":"be1b491c6ea33f10c2ea2ffd45d5e61915afebab040667003e7c372e80140ac1","strict":false,"support":"-","variadic":"-","volatility":"v"},"control_plane.workflow_evidence_delivery_operation(text,text)":{"acl":[["control_plane_owner","control_plane_owner","EXECUTE",false],["control_plane_runtime","control_plane_owner","EXECUTE",false]],"allTypes":null,"argumentModes":null,"argumentNames":["p_profile","p_operation"],"config":["search_path=pg_catalog"],"defaultExpression":null,"defaults":0,"inputTypes":["text","text"],"kind":"f","language":"sql","leakproof":false,"owner":"control_plane_owner","parallel":"u","result":"boolean","resultDefinition":"boolean","securityDefiner":false,"set":false,"sourceMD5":"bb5a26aa9776ece834d5aee67768cddf","sourceSHA256":"2bc2f07db5be45b90b7052da22cbb73cf884605240060916d8754afa5c122523","strict":false,"support":"-","variadic":"-","volatility":"i"},"control_plane.workflow_evidence_execution_assigned(uuid,uuid,uuid)":{"acl":[["control_plane_owner","control_plane_owner","EXECUTE",false],["control_plane_runtime","control_plane_owner","EXECUTE",false]],"allTypes":null,"argumentModes":null,"argumentNames":["p_tenant","p_node","p_invocation"],"config":["search_path=pg_catalog, control_plane"],"defaultExpression":null,"defaults":0,"inputTypes":["uuid","uuid","uuid"],"kind":"f","language":"sql","leakproof":false,"owner":"control_plane_owner","parallel":"u","result":"boolean","resultDefinition":"boolean","securityDefiner":false,"set":false,"sourceMD5":"f38f1e6f9b6f5d0426b992d169642b13","sourceSHA256":"485a06162c0644d6c91fcc0ef1da5cb6fa6e2737d4fe8397c3747eff4f9ee591","strict":false,"support":"-","variadic":"-","volatility":"s"},"control_plane.workflow_evidence_intent_valid(uuid,uuid,uuid,uuid,jsonb,jsonb)":{"acl":[["control_plane_owner","control_plane_owner","EXECUTE",false],["control_plane_runtime","control_plane_owner","EXECUTE",false]],"allTypes":null,"argumentModes":null,"argumentNames":["p_tenant","p_anchor","p_origin","p_invocation","p_record","p_requirement"],"config":["search_path=pg_catalog, control_plane"],"defaultExpression":null,"defaults":0,"inputTypes":["uuid","uuid","uuid","uuid","jsonb","jsonb"],"kind":"f","language":"sql","leakproof":false,"owner":"control_plane_owner","parallel":"u","result":"boolean","resultDefinition":"boolean","securityDefiner":false,"set":false,"sourceMD5":"21e14acf595ce49ec3d447386544a982","sourceSHA256":"f9d46b9aed546087fbb4960231cfb6ffaea8e9da8891f940b5955851c828620a","strict":false,"support":"-","variadic":"-","volatility":"s"},"control_plane.workflow_evidence_outcome_valid(uuid,uuid,jsonb,jsonb)":{"acl":[["control_plane_owner","control_plane_owner","EXECUTE",false],["control_plane_runtime","control_plane_owner","EXECUTE",false]],"allTypes":null,"argumentModes":null,"argumentNames":["p_tenant","p_anchor","p_entry","p_requirements"],"config":["search_path=pg_catalog, control_plane"],"defaultExpression":null,"defaults":0,"inputTypes":["uuid","uuid","jsonb","jsonb"],"kind":"f","language":"plpgsql","leakproof":false,"owner":"control_plane_owner","parallel":"u","result":"boolean","resultDefinition":"boolean","securityDefiner":false,"set":false,"sourceMD5":"936275d8c7170cd2057ca1165f84a872","sourceSHA256":"b246e2bfb60861fbc12e8dc71c5db351d6ba43303d90e3d8ccc737beaaba01b1","strict":false,"support":"-","variadic":"-","volatility":"s"},"control_plane.workflow_evidence_retains_revision(uuid,uuid,uuid)":{"acl":[["control_plane_owner","control_plane_owner","EXECUTE",false]],"allTypes":null,"argumentModes":null,"argumentNames":["p_tenant","p_artifact","p_revision"],"config":["search_path=pg_catalog"],"defaultExpression":null,"defaults":0,"inputTypes":["uuid","uuid","uuid"],"kind":"f","language":"sql","leakproof":false,"owner":"control_plane_owner","parallel":"u","result":"boolean","resultDefinition":"boolean","securityDefiner":false,"set":false,"sourceMD5":"695139aadf8ca154b86b5cd7cbac4df1","sourceSHA256":"cc94d69b52a28fb29f37feab7c2170a286b0194445a013c7f0b14a5c534bde07","strict":false,"support":"-","variadic":"-","volatility":"s"},"control_plane.workflow_native_receipt_proof(uuid,uuid,uuid,uuid,uuid,text)":{"acl":[["control_plane_owner","control_plane_owner","EXECUTE",false],["control_plane_runtime","control_plane_owner","EXECUTE",false]],"allTypes":["uuid","uuid","uuid","uuid","uuid","text","text","jsonb","jsonb","text","text","text","text"],"argumentModes":["i","i","i","i","i","i","t","t","t","t","t","t","t"],"argumentNames":["p_organization","p_node","p_root","p_revision","p_lease","p_receipt","operation","bounded_input","resource_scope","result_summary","response_digest","provider_effect_ref","lineage_failure"],"config":["search_path=pg_catalog, control_plane"],"defaultExpression":null,"defaults":0,"inputTypes":["uuid","uuid","uuid","uuid","uuid","text"],"kind":"f","language":"plpgsql","leakproof":false,"owner":"control_plane_owner","parallel":"u","result":"record","resultDefinition":"TABLE(operation text, bounded_input jsonb, resource_scope jsonb, result_summary text, response_digest text, provider_effect_ref text, lineage_failure text)","securityDefiner":false,"set":true,"sourceMD5":"0ddc47cbef1ef5594bba792b3dcdfd35","sourceSHA256":"f62172cc6c286f0266ba91669b0fcebe0b4be13673f94d49f7127e62c6a7fadd","strict":false,"support":"-","variadic":"-","volatility":"v"},"control_plane.workflow_pdlc_author_slot(jsonb,text,jsonb,jsonb)":{"acl":[["control_plane_owner","control_plane_owner","EXECUTE",false],["control_plane_runtime","control_plane_owner","EXECUTE",false]],"allTypes":null,"argumentModes":null,"argumentNames":["p_requirement","p_operation","p_scope","p_input"],"config":["search_path=pg_catalog"],"defaultExpression":null,"defaults":0,"inputTypes":["jsonb","text","jsonb","jsonb"],"kind":"f","language":"plpgsql","leakproof":false,"owner":"control_plane_owner","parallel":"u","result":"text","resultDefinition":"text","securityDefiner":false,"set":false,"sourceMD5":"0b7fdc137f2570b88ba96dd18ee1d510","sourceSHA256":"5831778199839bd07c7a7a806fab58ceed4833a4b550ee7e8cb48187bc5e097b","strict":false,"support":"-","variadic":"-","volatility":"i"},"control_plane.workflow_pdlc_file_desired_digest(text,text,jsonb)":{"acl":[["control_plane_owner","control_plane_owner","EXECUTE",false],["control_plane_runtime","control_plane_owner","EXECUTE",false]],"allTypes":null,"argumentModes":null,"argumentNames":["p_result_key","p_scope_digest","p_input"],"config":["search_path=pg_catalog"],"defaultExpression":null,"defaults":0,"inputTypes":["text","text","jsonb"],"kind":"f","language":"plpgsql","leakproof":false,"owner":"control_plane_owner","parallel":"u","result":"text","resultDefinition":"text","securityDefiner":false,"set":false,"sourceMD5":"40873996ea381ba3ff60c608d7cd2011","sourceSHA256":"9515440e92f8c01d2d75ea48fd37a859c529a706f15da0b34fb11305581d4977","strict":false,"support":"-","variadic":"-","volatility":"i"},"control_plane.workflow_pdlc_file_epoch_digest(text,text)":{"acl":[["control_plane_owner","control_plane_owner","EXECUTE",false],["control_plane_runtime","control_plane_owner","EXECUTE",false]],"allTypes":null,"argumentModes":null,"argumentNames":["p_desired","p_predecessor"],"config":["search_path=pg_catalog"],"defaultExpression":null,"defaults":0,"inputTypes":["text","text"],"kind":"f","language":"sql","leakproof":false,"owner":"control_plane_owner","parallel":"u","result":"text","resultDefinition":"text","securityDefiner":false,"set":false,"sourceMD5":"b0863a78f87ec5bfc70521c9b3d20231","sourceSHA256":"e9a3d0c1cfb82b8756cc48a30ea93cc4076926aa4eb2758d03cabcb61ded11f1","strict":false,"support":"-","variadic":"-","volatility":"i"},"control_plane.workflow_pdlc_file_key(text,text)":{"acl":[["control_plane_owner","control_plane_owner","EXECUTE",false],["control_plane_runtime","control_plane_owner","EXECUTE",false]],"allTypes":null,"argumentModes":null,"argumentNames":["p_result_key","p_path"],"config":["search_path=pg_catalog"],"defaultExpression":null,"defaults":0,"inputTypes":["text","text"],"kind":"f","language":"sql","leakproof":false,"owner":"control_plane_owner","parallel":"u","result":"text","resultDefinition":"text","securityDefiner":false,"set":false,"sourceMD5":"5b57a61050ae5779126623d7693e883c","sourceSHA256":"5d78e7de495072b90d25b6208eb92eb201c8ae9c13b045674777d42e44dfd640","strict":false,"support":"-","variadic":"-","volatility":"i"},"control_plane.workflow_pdlc_intent_valid(uuid,uuid,uuid,uuid,jsonb,jsonb)":{"acl":[["control_plane_owner","control_plane_owner","EXECUTE",false],["control_plane_runtime","control_plane_owner","EXECUTE",false]],"allTypes":null,"argumentModes":null,"argumentNames":["p_tenant","p_anchor","p_origin","p_invocation","p_record","p_requirement"],"config":["search_path=pg_catalog, control_plane"],"defaultExpression":null,"defaults":0,"inputTypes":["uuid","uuid","uuid","uuid","jsonb","jsonb"],"kind":"f","language":"plpgsql","leakproof":false,"owner":"control_plane_owner","parallel":"u","result":"boolean","resultDefinition":"boolean","securityDefiner":false,"set":false,"sourceMD5":"d8555a79c977cd437dbef29a45472eba","sourceSHA256":"1ee34a451813d9abb5c9fa3fd7fcd80c7bcbb9f00b070164b27fd026610cb338","strict":false,"support":"-","variadic":"-","volatility":"s"},"control_plane.workflow_pdlc_outcome_valid(uuid,uuid,jsonb,jsonb)":{"acl":[["control_plane_owner","control_plane_owner","EXECUTE",false],["control_plane_runtime","control_plane_owner","EXECUTE",false]],"allTypes":null,"argumentModes":null,"argumentNames":["p_organization","p_anchor","p_entry","p_requirements"],"config":["search_path=pg_catalog, control_plane"],"defaultExpression":null,"defaults":0,"inputTypes":["uuid","uuid","jsonb","jsonb"],"kind":"f","language":"plpgsql","leakproof":false,"owner":"control_plane_owner","parallel":"u","result":"boolean","resultDefinition":"boolean","securityDefiner":false,"set":false,"sourceMD5":"dafc8c0e1ca57ef52ed42911dde316a5","sourceSHA256":"51bcff7b1dbba237ecebe9bcd4f4eb708c28ff498f265f2e3181e9437e779fcf","strict":false,"support":"-","variadic":"-","volatility":"v"},"control_plane.workflow_pdlc_read_order(uuid,text,text,text)":{"acl":[["control_plane_owner","control_plane_owner","EXECUTE",false],["control_plane_runtime","control_plane_owner","EXECUTE",false]],"allTypes":null,"argumentModes":null,"argumentNames":["p_organization","p_write","p_head","p_content"],"config":["search_path=pg_catalog, control_plane"],"defaultExpression":null,"defaults":0,"inputTypes":["uuid","text","text","text"],"kind":"f","language":"sql","leakproof":false,"owner":"control_plane_owner","parallel":"u","result":"boolean","resultDefinition":"boolean","securityDefiner":false,"set":false,"sourceMD5":"9fdff248e9773cc3551afff38df5f599","sourceSHA256":"9569560201e12b79316b4fbaf27fe35075b286cfac5cd41826b6e4bb1103b9cc","strict":false,"support":"-","variadic":"-","volatility":"s"},"control_plane.workflow_pdlc_receipt_fact(uuid,uuid,uuid,uuid,uuid,text,text)":{"acl":[["control_plane_owner","control_plane_owner","EXECUTE",false],["control_plane_runtime","control_plane_owner","EXECUTE",false]],"allTypes":null,"argumentModes":null,"argumentNames":["p_organization","p_node","p_root","p_revision","p_lease","p_receipt","p_repository"],"config":["search_path=pg_catalog, control_plane"],"defaultExpression":null,"defaults":0,"inputTypes":["uuid","uuid","uuid","uuid","uuid","text","text"],"kind":"f","language":"plpgsql","leakproof":false,"owner":"control_plane_owner","parallel":"u","result":"jsonb","resultDefinition":"jsonb","securityDefiner":false,"set":false,"sourceMD5":"0f3c2939f9208dcac4784855b88bf371","sourceSHA256":"47781d03fc4157d8f69e7fea688716d5df41473047f520971c6564aa446d8575","strict":false,"support":"-","variadic":"-","volatility":"v"},"control_plane.workflow_pdlc_result_digest(text[])":{"acl":[["control_plane_owner","control_plane_owner","EXECUTE",false],["control_plane_runtime","control_plane_owner","EXECUTE",false]],"allTypes":null,"argumentModes":null,"argumentNames":["p_parts"],"config":["search_path=pg_catalog"],"defaultExpression":null,"defaults":0,"inputTypes":["text[]"],"kind":"f","language":"plpgsql","leakproof":false,"owner":"control_plane_owner","parallel":"u","result":"text","resultDefinition":"text","securityDefiner":false,"set":false,"sourceMD5":"f1643ab13f9b11750746a4271223d779","sourceSHA256":"c0733253fac2b7e52cbdfa41e21c5c5b9a0cff0ff08efa0c37ce1a7ffef31926","strict":false,"support":"-","variadic":"-","volatility":"i"},"control_plane.workflow_pdlc_result_valid(jsonb,jsonb,jsonb[])":{"acl":[["control_plane_owner","control_plane_owner","EXECUTE",false],["control_plane_runtime","control_plane_owner","EXECUTE",false]],"allTypes":null,"argumentModes":null,"argumentNames":["p_requirement","p_fact","p_proofs"],"config":["search_path=pg_catalog, control_plane"],"defaultExpression":null,"defaults":0,"inputTypes":["jsonb","jsonb","jsonb[]"],"kind":"f","language":"plpgsql","leakproof":false,"owner":"control_plane_owner","parallel":"u","result":"boolean","resultDefinition":"boolean","securityDefiner":false,"set":false,"sourceMD5":"e605964880369802a929370f3cf32eb7","sourceSHA256":"00eb403c68600d4d7e7a6241c2ae6374f4ab4b39e143406e9fa34f38ac9db092","strict":false,"support":"-","variadic":"-","volatility":"i"},"control_plane.workflow_receipt_effect_proof(uuid,uuid,uuid,uuid,uuid,text)":{"acl":[["control_plane_owner","control_plane_owner","EXECUTE",false],["control_plane_runtime","control_plane_owner","EXECUTE",false]],"allTypes":["uuid","uuid","uuid","uuid","uuid","text","text","jsonb","jsonb","text","text","text","text"],"argumentModes":["i","i","i","i","i","i","t","t","t","t","t","t","t"],"argumentNames":["p_organization","p_node","p_root","p_revision","p_lease","p_receipt","operation","bounded_input","resource_scope","result_summary","response_digest","provider_effect_ref","lineage_failure"],"config":["search_path=pg_catalog, control_plane"],"defaultExpression":null,"defaults":0,"inputTypes":["uuid","uuid","uuid","uuid","uuid","text"],"kind":"f","language":"sql","leakproof":false,"owner":"control_plane_owner","parallel":"u","result":"record","resultDefinition":"TABLE(operation text, bounded_input jsonb, resource_scope jsonb, result_summary text, response_digest text, provider_effect_ref text, lineage_failure text)","securityDefiner":false,"set":true,"sourceMD5":"2412f777ad7c6486f07b5639444e437c","sourceSHA256":"1b8bb38886ee5a2f5bb5d0eb224e89952ec3df449647c82b7f3177d5b2e003a7","strict":false,"support":"-","variadic":"-","volatility":"v"},"control_plane.workflow_receipt_lineage_proof(uuid,uuid,uuid,uuid,uuid,text)":{"acl":[["control_plane_owner","control_plane_owner","EXECUTE",false],["control_plane_runtime","control_plane_owner","EXECUTE",false]],"allTypes":["uuid","uuid","uuid","uuid","uuid","text","text","jsonb","jsonb","text","text","text","text"],"argumentModes":["i","i","i","i","i","i","t","t","t","t","t","t","t"],"argumentNames":["p_organization","p_node","p_root","p_revision","p_lease","p_receipt","operation","bounded_input","resource_scope","result_summary","response_digest","provider_effect_ref","lineage_failure"],"config":["search_path=pg_catalog, control_plane"],"defaultExpression":null,"defaults":0,"inputTypes":["uuid","uuid","uuid","uuid","uuid","text"],"kind":"f","language":"sql","leakproof":false,"owner":"control_plane_owner","parallel":"u","result":"record","resultDefinition":"TABLE(operation text, bounded_input jsonb, resource_scope jsonb, result_summary text, response_digest text, provider_effect_ref text, lineage_failure text)","securityDefiner":false,"set":true,"sourceMD5":"f3ae4c35bdf3ebbb9f01f4c721fda23a","sourceSHA256":"ac51fff3ebcbf1d8ea8cc851b44d7c63b680585669b35930c78d518a1bed2c5a","strict":false,"support":"-","variadic":"-","volatility":"v"}},"triggers":{"control_plane.run_nodes.workflow_attempt_journal_guard":["control_plane.guard_workflow_attempt_journal()",23,"O",false,false,false,0,"",null,"",null,null,false,false],"control_plane.run_nodes.workflow_effect_records_guard":["control_plane.guard_workflow_effect_records()",31,"O",false,false,false,0,"",null,"",null,null,false,false]}}$catalog$::jsonb THEN
    RAISE EXCEPTION 'workflow PDLC schema catalog changed';
 END IF;
END $workflow_pdlc_catalog$;

GRANT SELECT ON control_plane.session_workspace_storage,control_plane.session_archives,control_plane.session_archive_tasks,control_plane.session_workspace_volumes,control_plane.session_workspace_volume_tasks,control_plane.session_workspace_volume_attempts TO kodex_backup_reader;
ALTER DEFAULT PRIVILEGES FOR ROLE control_plane_owner IN SCHEMA control_plane GRANT SELECT ON TABLES TO kodex_backup_reader;
