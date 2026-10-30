-- Generated: generate-workflow-pdlc-proof.py; exact031 без расширения030.
-- Generated: generate-github-pr-proof.py; exact029 и отдельный PR admission.
-- Generated: generate-github-autonomous-proof.py; полный028 и exact GitHub admission.
-- Generated: generate-workspace-restore-proof.py; actual restore и inverse017+SSH проверяются отдельно.
-- Generated: generate-workspace-volume-ssh-proof.py; actual017+SSH и inverse014 не смешиваются.
-- Полный predecessor013 и metadata014 проверяются в одном READ ONLY snapshot.
-- predecessor-source-sha256: 6755e2bc1a40ebf7d6de8240e714dfa11cb092472bb5e3ad287d2799dd2aee30
-- name: workflow_upstream_rework_migration_postcondition :one
-- Только права и schema metadata; имена LOGIN, credentials и строки контекста не выдаются.
BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout = '5s';
SET LOCAL lock_timeout = '500ms';
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
 OR md5(replace(v_source,v_extension,''))<>'7010547e11bc8d1e8f67ac56feaba030' THEN
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

WITH RECURSIVE ssh_columns(table_name,column_name,type_name,required,expected_default) AS (VALUES
 ('runtime_secrets','purpose','text',true,'''APPLICATION''::text'),
 ('integration_invocations','ssh_compose_snapshot','jsonb',false,NULL),
 ('integration_invocations','ssh_compose_deadline','timestamp with time zone',false,NULL),
 ('integration_invocations','ssh_compose_recovery','boolean',true,'false'),
 ('integration_invocations','ssh_compose_admission_blocked','boolean',true,'false'),
 ('integration_invocations','ssh_compose_admission_block_reason','text',true,$default$''::text$default$),
 ('integration_connection_tests','ssh_compose_snapshot','jsonb',false,NULL)
), ssh_column_rows AS (
 SELECT expected.*,a.attnum,a.atttypid,a.attnotnull,a.atthasdef,pg_get_expr(d.adbin,d.adrelid) AS actual_default,
 pg_get_userbyid(relation.relowner) AS owner
 FROM ssh_columns expected LEFT JOIN pg_class relation ON relation.oid=to_regclass('control_plane.'||expected.table_name)
 LEFT JOIN pg_attribute a ON a.attrelid=relation.oid AND a.attname=expected.column_name AND a.attnum>0 AND NOT a.attisdropped
 LEFT JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum
), ssh_checks(table_name,constraint_name,definition) AS (VALUES
 ('integration_definitions','integration_definitions_adapter_check',$expected$CHECK((adapter=ANY(ARRAY['SYNTHETIC_HTTP','GITHUB','GITLAB','JIRA','CONFLUENCE','EMAIL_HTTPS','MATTERMOST_INTERACTION','HTTPS_JSON_READ','OPENAPI_MCP','CONTEXT7','SSH_COMPOSE'])))$expected$),
 ('integration_grants','integration_grants_resource_kind_check',$expected$CHECK((resource_kind=ANY(ARRAY['SYNTHETIC_JOURNAL','GITHUB_REPOSITORY','GITLAB_PROJECT','JIRA_PROJECT','CONFLUENCE_SPACE','EMAIL_SENDER','MATTERMOST_CHANNEL','HTTPS_RESOURCE','SSH_TARGET'])))$expected$),
 ('integration_invocations','integration_invocations_resource_kind_check',$expected$CHECK((resource_kind=ANY(ARRAY['SYNTHETIC_JOURNAL','GITHUB_REPOSITORY','GITLAB_PROJECT','JIRA_PROJECT','CONFLUENCE_SPACE','EMAIL_SENDER','MATTERMOST_CHANNEL','HTTPS_RESOURCE','SSH_TARGET'])))$expected$),
 ('runtime_secrets','runtime_secrets_purpose_check',$expected$CHECK((purpose=ANY(ARRAY['APPLICATION','SSH_COMPOSE'])))$expected$),
 ('integration_invocations','ssh_compose_snapshot_bounds',$expected$CHECK(((ssh_compose_snapshotISNULL)OR((jsonb_typeof(ssh_compose_snapshot)='object')AND(octet_length((ssh_compose_snapshot))<=32768))))$expected$),
 ('integration_invocations','ssh_compose_admission_block_shape',$expected$CHECK((((NOTssh_compose_admission_blocked)AND(ssh_compose_admission_block_reason=''))OR(ssh_compose_admission_blockedAND(ssh_compose_admission_block_reason='CURRENT_ADMISSION_DENIED'))))$expected$),
 ('integration_connection_tests','ssh_compose_test_snapshot_bounds',$expected$CHECK(((ssh_compose_snapshotISNULL)OR((jsonb_typeof(ssh_compose_snapshot)='object')AND(octet_length((ssh_compose_snapshot))<=32768))))$expected$)
), ssh_check_rows AS (
 SELECT e.*,c.oid,c.convalidated,c.conenforced,c.connoinherit,
 regexp_replace(replace(pg_get_constraintdef(c.oid),'::text',''),'[[:space:]]','','g') AS actual_definition
 FROM ssh_checks e LEFT JOIN pg_constraint c ON c.conrelid=to_regclass('control_plane.'||e.table_name) AND c.conname=e.constraint_name AND c.contype='c'
), ssh_functions(signature,source_md5) AS (VALUES
 ('control_plane.guard_ssh_compose_snapshot()','eb414956bfcd23b3e22d5093b6882e7d'),
 ('control_plane.guard_ssh_compose_admission_block()','c504ff89154b91a03cfedd56dc5f9de2'),
 ('control_plane.guard_ssh_compose_secret_purpose()','da345f85ac1516dc7f2325bf43773c4f')
), ssh_function_rows AS (
 SELECT expected.*,p.* FROM ssh_functions expected LEFT JOIN pg_proc p ON p.oid=to_regprocedure(expected.signature)
), ssh_triggers(table_name,trigger_name,signature) AS (VALUES
 ('integration_invocations','ssh_compose_admission_block_immutable','control_plane.guard_ssh_compose_admission_block()'),
 ('integration_invocations','ssh_compose_invocation_snapshot_immutable','control_plane.guard_ssh_compose_snapshot()'),
 ('integration_connection_tests','ssh_compose_test_snapshot_immutable','control_plane.guard_ssh_compose_snapshot()'),
 ('runtime_secrets','ssh_compose_secret_purpose_irreversible','control_plane.guard_ssh_compose_secret_purpose()')
), ssh_trigger_rows AS (
 SELECT expected.*,t.* FROM ssh_triggers expected LEFT JOIN pg_trigger t ON t.tgrelid=to_regclass('control_plane.'||expected.table_name) AND t.tgname=expected.trigger_name
), workspace_nodes(id) AS (
 SELECT 'control_plane.projects'::regclass::oid UNION
 SELECT c.conrelid FROM workspace_nodes n JOIN pg_constraint c ON c.confrelid=n.id
 WHERE c.contype='f' AND c.connamespace='control_plane'::regnamespace
), workspace_node_proof AS (
 SELECT count(*) AS count,md5(string_agg(n.nspname||'.'||r.relname,E'\n' ORDER BY n.nspname,r.relname)) AS digest
 FROM workspace_nodes JOIN pg_class r ON r.oid=workspace_nodes.id JOIN pg_namespace n ON n.oid=r.relnamespace
), workspace_edge_proof AS (
 SELECT count(*) AS count,md5(string_agg(cn.nspname||'.'||cc.relname||'|'||c.conname||'|'||pn.nspname||'.'||pc.relname||'|'||c.confdeltype::text,
 E'\n' ORDER BY cn.nspname,cc.relname,c.conname)) AS digest
 FROM pg_constraint c JOIN pg_class cc ON cc.oid=c.conrelid JOIN pg_namespace cn ON cn.oid=cc.relnamespace
 JOIN pg_class pc ON pc.oid=c.confrelid JOIN pg_namespace pn ON pn.oid=pc.relnamespace
 WHERE c.contype='f' AND c.conrelid IN(SELECT id FROM workspace_nodes) AND c.confrelid IN(SELECT id FROM workspace_nodes)
), workspace_guards AS (
 SELECT format('IF v_nodes <> %s OR v_node_fingerprint <> %L THEN',n.count,n.digest) AS nodes,
 format('IF v_edges <> %s OR v_edge_fingerprint <> %L THEN',e.count,e.digest) AS edges
 FROM workspace_node_proof n CROSS JOIN workspace_edge_proof e
), runtime_members(oid) AS (
    SELECT oid FROM pg_roles WHERE rolname='control_plane_runtime'
    UNION
    SELECT membership.member FROM pg_auth_members membership JOIN runtime_members parent ON parent.oid=membership.roleid
), members AS (
    SELECT role.oid,role.rolcanlogin,role.rolsuper,role.rolcreaterole,role.rolcreatedb,role.rolbypassrls
    FROM pg_roles role JOIN runtime_members USING (oid)
), reachable AS (
    -- INHERIT FALSE не запрещает SET ROLE; проверяется и эта цепочка полномочий.
    SELECT role.oid,role.rolsuper,role.rolcreaterole,role.rolcreatedb,role.rolbypassrls FROM pg_roles role
    WHERE EXISTS (SELECT 1 FROM members member WHERE pg_has_role(member.oid,role.oid,'SET'))
), targets AS (
    SELECT oid,relowner,relacl FROM pg_class
    WHERE oid IN ('control_plane.project_purge_context'::regclass,'control_plane.project_purge_targets'::regclass)
), authorizer AS (
    SELECT oid,proowner,prosecdef,provolatile,proconfig,prorettype,prolang,prosrc,proacl
    FROM pg_proc WHERE oid=to_regprocedure('control_plane.project_purge_authorized()')
),
column_expected(table_name,column_name,type_name,required,default_expression) AS (VALUES
 ('run_nodes','workflow_step_origin_node_id','uuid',false,NULL),
 ('run_nodes','workflow_business_attempt','integer',true,'0'),
 ('run_nodes','workflow_captured_artifacts','jsonb',false,NULL),
 ('run_nodes','workflow_attempts','jsonb',true,'''[]''::jsonb'),
 ('run_nodes','workflow_inputs','jsonb',true,'''[]''::jsonb'),
 ('run_nodes','workflow_effect_intents','jsonb',true,'''[]''::jsonb'),
 ('run_nodes','workflow_effect_rejoins','jsonb',true,'''[]''::jsonb'),
 ('runs','workflow_exception','jsonb',false,NULL),
 ('runs','workflow_exception_history','jsonb',true,'''[]''::jsonb')
), column_rows AS (
 SELECT expected.*,a.attnum,a.atttypid,a.attnotnull,a.atthasdef,pg_get_expr(d.adbin,d.adrelid) AS actual_default,
 pg_get_userbyid(relation.relowner) AS owner
 FROM column_expected expected LEFT JOIN pg_class relation ON relation.oid=to_regclass('control_plane.'||expected.table_name)
 LEFT JOIN pg_attribute a ON a.attrelid=relation.oid AND a.attname=expected.column_name AND NOT a.attisdropped AND a.attnum>0
 LEFT JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum
), check_expected(table_name,constraint_name,definition) AS (VALUES
 ('run_nodes','workflow_business_attempt_bound',$expected$CHECK(((workflow_business_attempt>=0)AND(workflow_business_attempt<=4)))$expected$),
 ('run_nodes','workflow_capture_bound',$expected$CHECK(((workflow_captured_artifactsISNULL)OR((jsonb_typeof(workflow_captured_artifacts)='object')AND(octet_length((workflow_captured_artifacts))<=32768))))$expected$),
 ('run_nodes','workflow_journal_bounds',$expected$CHECK(((jsonb_typeof(workflow_attempts)='array')AND(jsonb_array_length(workflow_attempts)<=4)AND(jsonb_typeof(workflow_inputs)='array')AND(jsonb_array_length(workflow_inputs)<=8)AND(jsonb_typeof(workflow_effect_intents)='array')AND(jsonb_array_length(workflow_effect_intents)<=1000)AND(jsonb_typeof(workflow_effect_rejoins)='array')AND(jsonb_array_length(workflow_effect_rejoins)<=1000)))$expected$),
 ('run_nodes','workflow_effect_record_bounds',$expected$CHECK(((jsonb_array_length(workflow_effect_intents)<=16)AND(jsonb_array_length(workflow_effect_rejoins)<=16)))$expected$),
 ('runs','workflow_exception_history_bound',$expected$CHECK(((jsonb_typeof(workflow_exception_history)='array')AND(jsonb_array_length(workflow_exception_history)<=48)))$expected$)
), check_rows AS (
 SELECT e.*,c.oid,c.convalidated,c.conenforced,c.connoinherit,
 regexp_replace(replace(pg_get_constraintdef(c.oid),'::text',''),'[[:space:]]','','g') AS actual_definition
 FROM check_expected e LEFT JOIN pg_constraint c ON c.conrelid=to_regclass('control_plane.'||e.table_name)
 AND c.conname=e.constraint_name AND c.contype='c'
), origin_fk AS (
 SELECT c.oid FROM pg_constraint c
 WHERE c.conrelid='control_plane.run_nodes'::regclass AND c.conname='workflow_physical_origin_fk' AND c.contype='f'
 AND c.confrelid=c.conrelid AND c.convalidated AND c.conenforced AND c.condeferrable AND NOT c.condeferred
 AND c.confdeltype='a' AND c.confupdtype='a' AND c.confmatchtype='s'
 AND c.conkey=ARRAY[(SELECT attnum FROM pg_attribute WHERE attrelid=c.conrelid AND attname='workflow_step_origin_node_id')]::smallint[]
 AND c.confkey=ARRAY[(SELECT attnum FROM pg_attribute WHERE attrelid=c.conrelid AND attname='id')]::smallint[]
), function_expected(signature,source_md5,language,volatility,search_path) AS (VALUES
 ('control_plane.guard_workflow_effect_records()','7010547e11bc8d1e8f67ac56feaba030','plpgsql','v','pg_catalog,control_plane'),
 ('control_plane.guard_workflow_physical_lineage()','1f08cfef012d3d1f8f7228dc1c17f2b8','plpgsql','v','pg_catalog,control_plane'),
 ('control_plane.guard_workflow_capture()','d354f3ce4d158d3b07ab806e0836edde','plpgsql','v','pg_catalog,control_plane'),
 ('control_plane.guard_workflow_attempt_journal()','7f8dc9469eb755a7b7760e8d4d7fc2cd','plpgsql','v','pg_catalog,control_plane'),
 ('control_plane.guard_workflow_input_snapshot()','2808dc6b5f79dc36e31b912aad24921b','plpgsql','v','pg_catalog,control_plane'),
 ('control_plane.guard_workflow_exception_episodes()','464af424a0b0ad947ddb89aa2119c733','plpgsql','v','pg_catalog,control_plane'),
 ('control_plane.runtime_workflow_step(uuid,uuid)','40878ace3171345ad62227dae8e4eb71','sql','s','pg_catalog'),
 ('control_plane.workflow_captured_artifact_pins(uuid,uuid)','6dbed221a9e8882b4dc24eebc53a2d46','sql','s','pg_catalog'),
 ('control_plane.workflow_runtime_artifact_pins(uuid,uuid)','592e7a1b582f361964c3df922c866e66','sql','s','pg_catalog'),
 ('control_plane.runtime_workflow_input_visible(uuid,uuid,uuid)','81167a271fc622d27d447e7110208e3f','sql','s','pg_catalog')
), function_rows AS (
 SELECT expected.*,p.oid,CASE WHEN p.oid='control_plane.guard_workflow_effect_records()'::regprocedure THEN replace(p.prosrc,$combined_deployment$                    OR (requirement.value->>'Kind'='DEPLOYMENT' AND requirement.value->>'Operation'='ssh.compose.deploy'
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
                    $combined_deployment$,'') ELSE p.prosrc END AS prosrc,p.proowner,p.prosecdef,p.proconfig,p.provolatile,p.prolang,p.prorettype,p.proretset,pg_get_function_result(p.oid) AS result_type,p.proacl,p.proargnames
 FROM function_expected expected LEFT JOIN pg_proc p ON p.oid=to_regprocedure(expected.signature)
), trigger_expected(table_name,trigger_name,signature,kind) AS (VALUES
 ('run_nodes','workflow_effect_records_guard','control_plane.guard_workflow_effect_records()',31),
 ('run_nodes','workflow_physical_lineage_guard','control_plane.guard_workflow_physical_lineage()',31),
 ('run_nodes','workflow_capture_guard','control_plane.guard_workflow_capture()',23),
 ('run_nodes','workflow_attempt_journal_guard','control_plane.guard_workflow_attempt_journal()',23),
 ('run_nodes','workflow_input_snapshot_guard','control_plane.guard_workflow_input_snapshot()',23),
 ('runs','workflow_exception_episodes_guard','control_plane.guard_workflow_exception_episodes()',31)
), trigger_rows AS (
 SELECT e.*,t.oid,t.tgtype,t.tgenabled,t.tgattr,t.tgqual,t.tgnargs,t.tgisinternal,t.tgdeferrable,t.tgfoid
 FROM trigger_expected e LEFT JOIN pg_trigger t ON t.tgrelid=to_regclass('control_plane.'||e.table_name) AND t.tgname=e.trigger_name
), nodes(relation_id) AS (
 SELECT 'control_plane.projects'::regclass::oid UNION
 SELECT c.conrelid FROM nodes n JOIN pg_constraint c ON c.confrelid=n.relation_id
 WHERE c.contype='f' AND c.connamespace='control_plane'::regnamespace AND c.conrelid NOT IN ('control_plane.session_workspace_storage'::regclass,'control_plane.session_workspace_volumes'::regclass,'control_plane.session_workspace_volume_tasks'::regclass,'control_plane.session_workspace_volume_attempts'::regclass)
), edge_rows AS (
 SELECT cn.nspname||'.'||cc.relname||'|'||c.conname||'|'||pn.nspname||'.'||pc.relname||'|'||c.confdeltype::text AS pin,
 cn.nspname AS child_schema,cc.relname AS child_name,c.conname
 FROM pg_constraint c JOIN pg_class cc ON cc.oid=c.conrelid JOIN pg_namespace cn ON cn.oid=cc.relnamespace
 JOIN pg_class pc ON pc.oid=c.confrelid JOIN pg_namespace pn ON pn.oid=pc.relnamespace
 WHERE c.contype='f' AND c.confrelid IN(SELECT relation_id FROM nodes) AND c.conrelid IN(SELECT relation_id FROM nodes)
), node_proof AS (
 SELECT count(*) AS count,md5(string_agg(n.nspname||'.'||r.relname,E'\n' ORDER BY n.nspname,r.relname)) AS digest
 FROM nodes JOIN pg_class r ON r.oid=nodes.relation_id JOIN pg_namespace n ON n.oid=r.relnamespace
), edge_proof AS (
 SELECT count(*) AS count,md5(string_agg(pin,E'\n' ORDER BY child_schema,child_name,conname)) AS digest FROM edge_rows
), baseline_edges AS (
 SELECT count(*) AS count,md5(string_agg(pin,E'\n' ORDER BY child_schema,child_name,conname)) AS digest FROM edge_rows
 WHERE NOT(child_schema='control_plane' AND child_name='run_nodes' AND conname IN ('workflow_physical_origin_fk','legacy_rework_gate_fk','legacy_rework_origin_fk','workflow_rework_gate_fk'))
), server_edges AS (
 SELECT count(*) AS count,md5(string_agg(pin,E'\n' ORDER BY child_schema,child_name,conname)) AS digest FROM edge_rows
 WHERE NOT(child_schema='control_plane' AND child_name='run_nodes' AND conname IN ('legacy_rework_gate_fk','legacy_rework_origin_fk','workflow_rework_gate_fk'))
), purge_guard AS (
 SELECT replace(replace(pg_get_functiondef('control_plane.purge_project_database(uuid,uuid,text)'::regprocedure),(SELECT nodes FROM workspace_guards),'IF v_nodes <> 105 OR v_node_fingerprint <> ''f51a0de25bd942a250997775617eeaee'' THEN'),(SELECT edges FROM workspace_guards),format('IF v_edges <> 287 OR v_edge_fingerprint <> %L THEN',(SELECT digest FROM edge_proof))) AS definition
), legacy_function_rows AS (
 SELECT p.* FROM pg_proc p WHERE p.oid IN (
   to_regprocedure('control_plane.runtime_file_source_visible(uuid,uuid,uuid,uuid,uuid,text,text,uuid)'),
   to_regprocedure('control_plane.artifact_has_retained_revisions(uuid)'))
), rework_function_expected(signature,digest,language,volatility,returns_set,result_type) AS (VALUES
 ('control_plane.legacy_workflow_step_origin(uuid,uuid)','38643cea92849441e4410cb89542a4dc','sql','s',true,'uuid'),
 ('control_plane.guard_legacy_workflow_rework()','1e54e3fd2baa899d75ea376c80f02076','plpgsql','v',false,'trigger'),
 ('control_plane.guard_legacy_workflow_rework_edges()','3b7ade1091c3f36c7b6698153f6e3830','plpgsql','v',false,'trigger'),
 ('control_plane.legacy_rework_callback_origin(uuid,uuid,uuid)','b6c7670a1a25bde3515cd840d888c9b5','sql','s',true,'record'),
 ('control_plane.legacy_rework_execution_assigned(uuid,uuid)','c63830e6c39a5245e7c1875294d98aef','sql','s',false,'boolean'),
 ('control_plane.legacy_rework_delivery_pending(uuid,uuid)','c182b36bcd3cd642c5a29fa6dcbe4a01','sql','s',false,'boolean')
), rework_functions AS (
 SELECT expected.*,p.oid,p.prosrc,p.proowner,p.prosecdef,p.proconfig,p.provolatile,p.prolang,p.proretset,p.prorettype,p.proacl
 FROM rework_function_expected expected LEFT JOIN pg_proc p ON p.oid=to_regprocedure(expected.signature)
), rework_fk_expected(name,column_name,target_relation,target_column) AS (VALUES
 ('legacy_rework_gate_fk','owner_rework_gate_id','control_plane.owner_gates','id'),
 ('legacy_rework_origin_fk','owner_rework_origin_node_id','control_plane.run_nodes','id')
), rework_fks AS (
 SELECT expected.*,fk.oid,fk.contype,fk.confrelid,fk.conkey,fk.confkey,fk.confmatchtype,fk.confdeltype,fk.confupdtype,
 fk.convalidated,fk.conenforced,fk.condeferrable,fk.condeferred
 FROM rework_fk_expected expected LEFT JOIN pg_constraint fk ON fk.conrelid='control_plane.run_nodes'::regclass AND fk.conname=expected.name
), rework_index_expected(name,columns,predicate,is_unique) AS (VALUES
 ('run_nodes_owner_rework_once',ARRAY['owner_rework_gate_id'],
  '((owner_rework_gate_idISNOTNULL)AND(owner_rework_origin_node_idISNULL))',true),
 ('run_nodes_owner_rework_execution',ARRAY['owner_rework_origin_node_id','attempt'],
  '(owner_rework_origin_node_idISNOTNULL)',true),
 ('run_nodes_owner_rework_run',ARRAY['run_id'],'(owner_rework_gate_idISNOTNULL)',false)
), upstream_expected(signature,digest,language,volatility,result_type,search_path,returns_set,defaults) AS (VALUES
 ('control_plane.workflow_rework_floors(uuid,uuid)','a8ae73322d384f21d1ab8b9da612f629','sql','s','jsonb','pg_catalog,control_plane',false,0),
 ('control_plane.workflow_effective_receipts(uuid,uuid)','f83a1e6f43dbca02774ddf18de6bc67c','plpgsql','s','jsonb','pg_catalog,control_plane',false,0),
 ('control_plane.workflow_effective_inputs(uuid,uuid,text)','4ce13ff5d375f09417692214c052d758','plpgsql','s','jsonb','pg_catalog,control_plane',false,0),
 ('control_plane.workflow_rework_author(jsonb,text,text[])','6acd0001153d7aa5e36f6025df7b3f61','plpgsql','i','text','pg_catalog,control_plane',false,1),
 ('control_plane.workflow_irreversible_effect_exists(uuid,uuid,uuid)','cd45c720e7587f675fbb4239ab07b57a','sql','s','boolean','pg_catalog',false,0),
 ('control_plane.workflow_upstream_plan_valid(uuid,uuid,jsonb)','c40986bc595723890e2137683be0487c','plpgsql','s','boolean','pg_catalog,control_plane',false,0),
 ('control_plane.guard_workflow_rework_node()','ba0116973da3d92b6720e45cb5973c0f','plpgsql','v','trigger','pg_catalog,control_plane',false,0),
 ('control_plane.guard_workflow_superseded_gate()','f2bb455d04a66632f9d18440331d18cc','plpgsql','v','trigger','pg_catalog,control_plane',false,0),
 ('control_plane.workflow_reviewed_artifact_pins(uuid,uuid,jsonb)','9758fdb904d91adf1ce9929b85edc1e5','sql','s','TABLE(artifact_id uuid,revision_id uuid)','pg_catalog',true,0),
 ('control_plane.workflow_input_artifact_pins(uuid,uuid,jsonb)','f5c0dc78a51575588f0f24c5130011ef','sql','s','TABLE(artifact_id uuid,revision_id uuid)','pg_catalog',true,0),
 ('control_plane.workflow_rework_artifact_pins(uuid,uuid)','80e46f926f73883fc4feb38faa69d13f','sql','s','TABLE(artifact_id uuid,revision_id uuid)','pg_catalog',true,0),
 ('control_plane.workflow_public_attempt_results(uuid,uuid)','80b6457cc872824d87164494cd44dc1a','plpgsql','s','jsonb','pg_catalog,control_plane',false,0)
), base_proof AS (
SELECT jsonb_build_object(
    'readOnly',current_setting('transaction_read_only'),
    'database',current_database(),
    'gooseVersion',(SELECT max(version_id) FILTER (WHERE is_applied) FROM public.goose_db_version WHERE version_id<20261026001500),
    'tables',(SELECT count(*) FROM targets),
    'runtimeRoles',(SELECT count(*) FROM members),
    'runtimeLogins',(SELECT count(*) FROM members WHERE rolcanlogin),
    'tableOwners',(SELECT bool_and(pg_get_userbyid(relowner)='control_plane_owner') FROM targets),
    'runtimeRolesUnprivileged',NOT EXISTS (SELECT 1 FROM reachable WHERE rolsuper OR rolcreaterole OR rolcreatedb OR rolbypassrls
        OR pg_has_role(oid,'control_plane_owner','MEMBER'))
        AND NOT EXISTS (SELECT 1 FROM pg_auth_members membership JOIN reachable actor ON actor.oid=membership.member
            WHERE membership.admin_option),
    'runtimeRead',(SELECT bool_and(has_table_privilege(member.oid,target.oid,'SELECT')) FROM members member CROSS JOIN targets target),
    'runtimeMutationDenied',NOT EXISTS (
        SELECT 1 FROM reachable member CROSS JOIN targets target
        CROSS JOIN unnest(ARRAY['INSERT','UPDATE','DELETE','TRUNCATE','REFERENCES','TRIGGER','MAINTAIN']) privilege
        WHERE has_table_privilege(member.oid,target.oid,privilege)),
    'runtimeColumnMutationDenied',NOT EXISTS (
        SELECT 1 FROM reachable member CROSS JOIN targets target
        CROSS JOIN unnest(ARRAY['INSERT','UPDATE','REFERENCES']) privilege
        WHERE has_any_column_privilege(member.oid,target.oid,privilege)),
    'runtimeGrantOptionDenied',NOT EXISTS (SELECT 1 FROM reachable member CROSS JOIN targets target
        WHERE has_table_privilege(member.oid,target.oid,'SELECT WITH GRANT OPTION')),
    'publicPrivilegesRevoked',NOT EXISTS (
        SELECT 1 FROM targets target CROSS JOIN LATERAL aclexplode(coalesce(target.relacl,acldefault('r',target.relowner))) acl
        WHERE acl.grantee=0
        UNION ALL
        SELECT 1 FROM pg_attribute column_row JOIN targets target ON target.oid=column_row.attrelid
        CROSS JOIN LATERAL aclexplode(nullif(column_row.attacl,ARRAY[]::aclitem[])) acl WHERE acl.grantee=0),
    'ownerWrite',(SELECT bool_and(has_table_privilege('control_plane_owner',target.oid,privilege)) FROM targets target
        CROSS JOIN unnest(ARRAY['SELECT','INSERT','UPDATE','DELETE']) privilege),
    'authorizationFunction',EXISTS (
        SELECT 1 FROM authorizer function_row WHERE function_row.prosecdef AND function_row.provolatile='s'
            AND pg_get_userbyid(function_row.proowner)='control_plane_owner'
            AND function_row.proconfig=ARRAY['search_path=pg_catalog']
            AND function_row.prorettype='boolean'::regtype
            AND function_row.prolang=(SELECT oid FROM pg_language WHERE lanname='sql')
            AND function_row.prosrc=$body$
    SELECT EXISTS (
        SELECT 1 FROM control_plane.project_purge_context context
        WHERE context.transaction_id = pg_current_xact_id_if_assigned()
          AND context.backend_pid = pg_backend_pid()
    );
$body$
            AND has_function_privilege('control_plane_runtime',function_row.oid,'EXECUTE')
            AND NOT EXISTS (SELECT 1 FROM aclexplode(coalesce(function_row.proacl,acldefault('f',function_row.proowner))) acl
                WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE')),
    'contextsEmpty',NOT EXISTS (SELECT 1 FROM control_plane.project_purge_context)
        AND NOT EXISTS (SELECT 1 FROM control_plane.project_purge_targets)
,
 'serverWorkflowProfile','SERVER_DAG_V1',
 'bundleTableOwner',(SELECT pg_get_userbyid(relowner) FROM pg_class WHERE oid='control_plane.repository_bundle_publications'::regclass),
 'bundleNoBodyColumns',NOT EXISTS(SELECT 1 FROM pg_attribute
   WHERE attrelid='control_plane.repository_bundle_publications'::regclass AND attnum>0 AND NOT attisdropped
     AND (atttypid='bytea'::regtype OR attname IN('archive_bytes','manifest_bytes'))),
 'bundleForeignKeys',(SELECT count(*) FROM pg_constraint
   WHERE conrelid='control_plane.repository_bundle_publications'::regclass AND contype='f'),
 -- Удержание прежних13 FK включает реально действующие проверенные ограничения.
 'bundleDeferredForeignKeys',(SELECT count(*) FROM pg_constraint
   WHERE conrelid='control_plane.repository_bundle_publications'::regclass AND contype='f'
     AND condeferrable AND convalidated AND conenforced),
 'bundleGuard',EXISTS(SELECT 1 FROM pg_trigger guard JOIN pg_proc function_row ON function_row.oid=guard.tgfoid
   WHERE guard.tgrelid='control_plane.repository_bundle_publications'::regclass
     AND guard.tgname='repository_bundle_publication_guard'
     AND guard.tgfoid=to_regprocedure('control_plane.guard_repository_bundle_publication()')
     AND guard.tgtype=27 AND guard.tgenabled='O' AND guard.tgattr=''::int2vector
     AND guard.tgqual IS NULL AND guard.tgnargs=0 AND NOT guard.tgisinternal AND NOT guard.tgdeferrable
     AND pg_get_userbyid(function_row.proowner)='control_plane_owner' AND NOT function_row.prosecdef
     AND function_row.prorettype='trigger'::regtype AND NOT function_row.proretset
     AND function_row.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql')
     AND function_row.provolatile='v' AND function_row.proconfig IS NULL
     AND md5(function_row.prosrc)='14e949376306c955058e804dfcfa0855'),
 'workflowColumns',(SELECT count(*) FROM column_rows WHERE attnum IS NOT NULL),
 'workflowConstraints',(SELECT count(*) FROM check_rows WHERE oid IS NOT NULL)+(SELECT count(*) FROM origin_fk),
 'workflowGuards',(SELECT count(*) FROM trigger_rows WHERE oid IS NOT NULL),
 'workflowHelpers',(SELECT count(*) FROM function_rows WHERE signature NOT LIKE '%guard_%' AND oid IS NOT NULL),
 'schemaOwner',(SELECT bool_and(owner='control_plane_owner') FROM column_rows),
 'columns',(SELECT count(*)=9 AND bool_and(attnum IS NOT NULL AND atttypid=to_regtype(type_name) AND attnotnull=required
   AND actual_default IS NOT DISTINCT FROM default_expression AND atthasdef=(default_expression IS NOT NULL)) FROM column_rows),
 'constraints',(SELECT count(*)=5 AND bool_and(oid IS NOT NULL AND convalidated AND conenforced AND NOT connoinherit AND actual_definition=definition) FROM check_rows)
   AND (SELECT count(*)=1 FROM origin_fk),
 'indexes',EXISTS(SELECT 1 FROM pg_index i JOIN pg_class relation ON relation.oid=i.indexrelid JOIN pg_am am ON am.oid=relation.relam
   WHERE i.indexrelid=to_regclass('control_plane.run_nodes_workflow_step_once') AND i.indrelid='control_plane.run_nodes'::regclass
   AND i.indisvalid AND i.indisready AND i.indislive AND i.indisunique AND NOT i.indisprimary AND i.indnkeyatts=2 AND i.indnatts=2
   AND i.indexprs IS NULL AND am.amname='btree' AND i.indoption::text='0 0'
   AND pg_get_indexdef(i.indexrelid,1,true)='root_run_id' AND pg_get_indexdef(i.indexrelid,2,true)='workflow_step_key'
   AND regexp_replace(replace(pg_get_expr(i.indpred,i.indrelid),'::text',''),'[[:space:]]','','g')=$expected$((workflow_step_key<>'')AND(workflow_step_origin_node_idISNULL)AND(owner_rework_gate_idISNULL))$expected$)
 AND EXISTS(SELECT 1 FROM pg_index i JOIN pg_class relation ON relation.oid=i.indexrelid JOIN pg_am am ON am.oid=relation.relam
   WHERE i.indexrelid=to_regclass('control_plane.run_nodes_workflow_physical_attempt') AND i.indrelid='control_plane.run_nodes'::regclass
   AND i.indisvalid AND i.indisready AND i.indislive AND NOT i.indisunique AND NOT i.indisprimary AND i.indnkeyatts=3 AND i.indnatts=3
   AND i.indexprs IS NULL AND am.amname='btree' AND i.indoption::text='0 0 0'
   AND pg_get_indexdef(i.indexrelid,1,true)='workflow_step_origin_node_id' AND pg_get_indexdef(i.indexrelid,2,true)='workflow_business_attempt'
   AND pg_get_indexdef(i.indexrelid,3,true)='attempt'
   AND regexp_replace(pg_get_expr(i.indpred,i.indrelid),'[[:space:]]','','g')=$expected$(workflow_step_origin_node_idISNOTNULL)$expected$),
 'triggers',(SELECT count(*)=6 AND bool_and(oid IS NOT NULL AND tgtype=kind AND tgenabled='O' AND tgattr=''::int2vector
   AND tgqual IS NULL AND tgnargs=0 AND NOT tgisinternal AND NOT tgdeferrable AND tgfoid=to_regprocedure(signature)) FROM trigger_rows),
 'functionSources',(SELECT count(*)=10 AND bool_and(oid IS NOT NULL AND md5(prosrc)=source_md5) FROM function_rows),
 'functionMetadata',(SELECT count(*)=10 AND bool_and(oid IS NOT NULL AND pg_get_userbyid(proowner)='control_plane_owner'
   AND NOT prosecdef AND provolatile::text=volatility AND prolang=(SELECT oid FROM pg_language WHERE lanname=language)
   AND (signature<>'control_plane.workflow_runtime_artifact_pins(uuid,uuid)' OR proargnames=ARRAY['p_tenant','p_node','artifact_id','revision_id'])
   AND cardinality(proconfig)=1 AND regexp_replace(proconfig[1],'[[:space:]]','','g')='search_path='||search_path
   AND (CASE WHEN language='plpgsql' THEN prorettype='trigger'::regtype AND NOT proretset
     WHEN signature IN ('control_plane.workflow_captured_artifact_pins(uuid,uuid)','control_plane.workflow_runtime_artifact_pins(uuid,uuid)')
       THEN prorettype='record'::regtype AND proretset AND result_type='TABLE(artifact_id uuid, revision_id uuid)'
     ELSE prorettype='boolean'::regtype AND NOT proretset END)) FROM function_rows),
 'functionACL',(SELECT count(*)=10 AND bool_and(oid IS NOT NULL AND has_function_privilege('control_plane_runtime',oid,'EXECUTE')
   AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(proacl,acldefault('f',proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE')) FROM function_rows),
 'runtimeColumnAccess',(SELECT count(*)=9 AND bool_and(has_column_privilege('control_plane_runtime',to_regclass('control_plane.'||table_name),column_name,'SELECT')
   AND has_column_privilege('control_plane_runtime',to_regclass('control_plane.'||table_name),column_name,'UPDATE')) FROM column_expected),
 'purgeNodes',(SELECT count FROM node_proof),'purgeNodeFingerprint',(SELECT digest FROM node_proof),
 'purgeEdges',(SELECT count FROM edge_proof),'purgeEdgeFingerprint',(SELECT digest FROM edge_proof),
 'purgeBaselineEdges',(SELECT count FROM baseline_edges),'purgeBaselineEdgeFingerprint',(SELECT digest FROM baseline_edges),
 'purgeGraphGuard',(SELECT count=105 AND digest='f51a0de25bd942a250997775617eeaee' FROM node_proof)
   AND (SELECT count=284 AND digest='6bf6ad76d8c7ae66e07f75ecf719fdd6' FROM server_edges) AND (SELECT count=287 FROM edge_proof) AND (SELECT count=283 AND digest='be6e23d9e894687a69a19e9b5777ab09' FROM baseline_edges)
   AND (SELECT count(*)=1 FROM origin_fk)
   AND (SELECT regexp_count(definition,'IF v_nodes <> [0-9]+ OR v_node_fingerprint <> ''[a-f0-9]{32}'' THEN')=1
    AND regexp_count(definition,'IF v_edges <> [0-9]+ OR v_edge_fingerprint <> ''[a-f0-9]{32}'' THEN')=1
    AND position('IF v_nodes <> 105 OR v_node_fingerprint <> ''f51a0de25bd942a250997775617eeaee'' THEN' IN definition)>0
    AND position(format('IF v_edges <> 287 OR v_edge_fingerprint <> %L THEN',(SELECT digest FROM edge_proof)) IN definition)>0 FROM purge_guard)
,
 'visibilitySource',EXISTS(SELECT 1 FROM legacy_function_rows WHERE oid=to_regprocedure('control_plane.runtime_file_source_visible(uuid,uuid,uuid,uuid,uuid,text,text,uuid)')
 AND md5(prosrc)='be098f192205fa279eb55a502655052b'
 AND pg_get_userbyid(proowner)='control_plane_owner' AND NOT prosecdef AND provolatile='s'
 AND prolang=(SELECT oid FROM pg_language WHERE lanname='sql') AND prorettype='boolean'::regtype
 AND cardinality(proconfig)=1 AND regexp_replace(proconfig[1],'[[:space:]]','','g')='search_path=pg_catalog,control_plane'
 AND has_function_privilege('control_plane_runtime',oid,'EXECUTE')
 AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(proacl,acldefault('f',proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE')),
 'retentionSource',EXISTS(SELECT 1 FROM legacy_function_rows WHERE oid=to_regprocedure('control_plane.artifact_has_retained_revisions(uuid)')
 AND md5(prosrc)='29166a148eef617208fc527afaf907d1'
 AND pg_get_userbyid(proowner)='control_plane_owner' AND prosecdef AND provolatile='s'
 AND prolang=(SELECT oid FROM pg_language WHERE lanname='sql') AND prorettype='boolean'::regtype
 AND cardinality(proconfig)=1 AND regexp_replace(proconfig[1],'[[:space:]]','','g')='search_path=pg_catalog'
 AND has_function_privilege('control_plane_runtime',oid,'EXECUTE')
 AND has_function_privilege('artifact_retention_runtime',oid,'EXECUTE')
 AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(proacl,acldefault('f',proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE'))
) AS proof
), predecessor_proof AS (
SELECT (base_proof.proof || jsonb_build_object(
'workflowExceptionContextProfile','SERVER_EXCEPTION_CONTEXT_V1',
 'workflowContextFunctions',(SELECT count(*)=2 AND bool_and(p.oid IS NOT NULL AND md5(p.prosrc)=expected.digest
   AND pg_get_userbyid(p.proowner)='control_plane_owner' AND NOT p.prosecdef AND p.provolatile='s'
   AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='sql') AND p.prorettype='record'::regtype AND p.proretset
   AND pg_get_function_result(p.oid)=expected.result AND p.proconfig=ARRAY['search_path=pg_catalog']
   AND has_function_privilege('control_plane_runtime',p.oid,'EXECUTE')
   AND NOT EXISTS (SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl
      WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE'))
   FROM (VALUES
('control_plane.workflow_exception_context_node(uuid,uuid)','3cc8dec18528a675560628e1d30d1ab0','TABLE(root_id uuid, project_id uuid, actor_id uuid, source_node_id uuid, inputs jsonb, dependency_keys jsonb)'),
('control_plane.workflow_exception_artifact_pins(uuid,uuid)','00d0eed13a485103d2415e656a561bce','TABLE(artifact_id uuid, revision_id uuid)')
   ) expected(signature,digest,result) LEFT JOIN pg_proc p ON p.oid=to_regprocedure(expected.signature)),
 'workflowContextExactFunctions',(SELECT count(*)=2 FROM pg_proc WHERE pronamespace='control_plane'::regnamespace
   AND proname IN ('workflow_exception_context_node','workflow_exception_artifact_pins')),
'legacyReworkProfile','LEGACY_OWNER_REWORK_V1',
 'legacyPurgeSource',EXISTS (SELECT 1 FROM pg_proc function_row
 WHERE function_row.oid='control_plane.purge_project_database(uuid,uuid,text)'::regprocedure
 AND md5(replace(replace(replace(function_row.prosrc,(SELECT nodes FROM workspace_guards),'IF v_nodes <> 105 OR v_node_fingerprint <> ''f51a0de25bd942a250997775617eeaee'' THEN'),(SELECT edges FROM workspace_guards),format('IF v_edges <> 287 OR v_edge_fingerprint <> %L THEN',(SELECT digest FROM edge_proof))),format('IF v_edges <> 287 OR v_edge_fingerprint <> %L THEN',(SELECT digest FROM edge_proof)),
 'IF v_edges <> 284 OR v_edge_fingerprint <> ''6bf6ad76d8c7ae66e07f75ecf719fdd6'' THEN'))='b886c24d4e2f5c4a0ec08123d99462b0'
 AND pg_get_userbyid(function_row.proowner)='control_plane_owner' AND function_row.prosecdef
 AND function_row.provolatile='v' AND function_row.prorettype='integer'::regtype AND NOT function_row.proretset
 AND function_row.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql')
 AND function_row.proconfig=ARRAY['search_path=pg_catalog']
 AND has_function_privilege('control_plane_runtime',function_row.oid,'EXECUTE')
 AND NOT EXISTS (SELECT 1 FROM aclexplode(coalesce(function_row.proacl,acldefault('f',function_row.proowner))) acl
     WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE')),
 'legacyColumns',(SELECT count(*)=2 AND bool_and(a.atttypid='uuid'::regtype AND NOT a.attnotnull AND NOT a.atthasdef)
   FROM pg_attribute a WHERE a.attrelid='control_plane.run_nodes'::regclass AND a.attnum>0 AND NOT a.attisdropped
     AND a.attname IN ('owner_rework_gate_id','owner_rework_origin_node_id')),
 'legacyForeignKeys',(SELECT count(*)=2 AND bool_and(oid IS NOT NULL AND contype='f' AND confrelid=to_regclass(target_relation)
   AND convalidated AND conenforced AND condeferrable AND NOT condeferred AND confmatchtype='s' AND confdeltype='a' AND confupdtype='a'
   AND conkey=ARRAY[(SELECT attnum FROM pg_attribute WHERE attrelid='control_plane.run_nodes'::regclass AND attname=column_name)]::smallint[]
   AND confkey=ARRAY[(SELECT attnum FROM pg_attribute WHERE attrelid=to_regclass(target_relation) AND attname=target_column)]::smallint[]) FROM rework_fks),
 'legacyConstraint',EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='control_plane.run_nodes'::regclass
   AND conname='legacy_rework_origin_requires_gate' AND contype='c' AND convalidated AND conenforced AND NOT connoinherit
   AND regexp_replace(pg_get_constraintdef(oid),'[[:space:]]','','g')='CHECK(((owner_rework_origin_node_idISNULL)OR(owner_rework_gate_idISNOTNULL)))'),
 'legacyIndexes',(SELECT count(*)=3 AND bool_and(EXISTS(SELECT 1 FROM pg_index i JOIN pg_class relation ON relation.oid=i.indexrelid
   JOIN pg_am am ON am.oid=relation.relam WHERE i.indexrelid=to_regclass('control_plane.'||e.name)
     AND i.indrelid='control_plane.run_nodes'::regclass AND i.indisvalid AND i.indisready AND i.indislive AND i.indisunique=e.is_unique
     AND NOT i.indisprimary AND i.indnkeyatts=cardinality(e.columns) AND i.indnatts=cardinality(e.columns)
     AND i.indexprs IS NULL AND am.amname='btree'
     AND (SELECT array_agg(pg_get_indexdef(i.indexrelid,position,true) ORDER BY position)
       FROM generate_series(1,cardinality(e.columns)) position)=e.columns
     AND NOT EXISTS(SELECT 1 FROM unnest(i.indoption) option WHERE option<>0)
     AND regexp_replace(pg_get_expr(i.indpred,i.indrelid),'[[:space:]]','','g')=e.predicate)) FROM rework_index_expected e),
 'legacyTriggers',(SELECT count(*)=3 AND bool_and(EXISTS(SELECT 1 FROM pg_trigger t WHERE t.tgrelid=to_regclass('control_plane.'||e.relation)
   AND t.tgname=e.name AND t.tgfoid=to_regprocedure(e.signature) AND t.tgtype=e.kind AND t.tgenabled='O'
   AND t.tgattr=''::int2vector AND t.tgqual IS NULL AND t.tgnargs=0 AND NOT t.tgisinternal
   AND t.tgdeferrable=e.deferred AND t.tginitdeferred=e.deferred)) FROM (VALUES
     ('run_nodes','legacy_workflow_rework_guard','control_plane.guard_legacy_workflow_rework()',31,false),
     ('run_nodes','legacy_workflow_rework_edges_guard','control_plane.guard_legacy_workflow_rework_edges()',21,true),
     ('runs','legacy_workflow_rework_run_guard','control_plane.guard_legacy_workflow_rework()',27,false)) e(relation,name,signature,kind,deferred)),
 'legacyFunctions',(SELECT count(*)=6 AND bool_and(oid IS NOT NULL AND md5(prosrc)=digest AND NOT prosecdef
   AND pg_get_userbyid(proowner)='control_plane_owner' AND provolatile::text=volatility
   AND prolang=(SELECT oid FROM pg_language WHERE lanname=language) AND proretset=returns_set AND prorettype=to_regtype(result_type)
   AND cardinality(proconfig)=1 AND regexp_replace(proconfig[1],'[[:space:]]','','g')='search_path=pg_catalog,control_plane'
   AND pg_get_function_result(oid)=CASE WHEN signature='control_plane.legacy_rework_callback_origin(uuid,uuid,uuid)'
     THEN 'TABLE(eligible boolean, original_node_id uuid, final_node_id uuid, callback_edge_id uuid, callback_edge_ref text, parent_node_id uuid, parent_node_ref text, parent_run_id uuid, revision_ref text, runtime_attempt integer, generation bigint)'
     WHEN returns_set THEN 'SETOF uuid' ELSE result_type END
   AND has_function_privilege('control_plane_runtime',oid,'EXECUTE')
   AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(proacl,acldefault('f',proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE')) FROM rework_functions),
 'legacyColumnAccess',(SELECT bool_and(has_column_privilege('control_plane_runtime','control_plane.run_nodes',name,'SELECT')
   AND has_column_privilege('control_plane_runtime','control_plane.run_nodes',name,'UPDATE'))
   FROM unnest(ARRAY['owner_rework_gate_id','owner_rework_origin_node_id']) name),
 'serverPurgeEdges',(SELECT count FROM server_edges),'serverPurgeFingerprint',(SELECT digest FROM server_edges)
,
 'workflowUpstreamReworkProfile','OWNER_APPROVED_UPSTREAM_V1',
 'upstreamFunctions',(SELECT count(*)=12 AND bool_and(p.oid IS NOT NULL AND md5(p.prosrc)=expected.digest
   AND p.proowner='control_plane_owner'::regrole AND NOT p.prosecdef AND NOT p.proleakproof AND NOT p.proisstrict
   AND p.prokind='f' AND p.proparallel='u' AND p.provolatile::text=expected.volatility
   AND p.prolang=(SELECT oid FROM pg_language WHERE lanname=expected.language)
   AND p.proretset=expected.returns_set AND p.pronargdefaults=expected.defaults
   AND regexp_replace(pg_get_function_result(p.oid),'[[:space:]]','','g')=regexp_replace(expected.result_type,'[[:space:]]','','g')
   AND cardinality(p.proconfig)=1 AND regexp_replace(p.proconfig[1],'[[:space:]]','','g')='search_path='||expected.search_path
   AND (expected.defaults=0 OR pg_get_expr(p.proargdefaults,0)='ARRAY[]::text[]')
   AND has_function_privilege('control_plane_runtime',p.oid,'EXECUTE')
   AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl
      WHERE acl.grantee NOT IN ('control_plane_owner'::regrole,'control_plane_runtime'::regrole)
         OR (acl.grantee<>'control_plane_owner'::regrole AND acl.is_grantable)))
   FROM upstream_expected expected LEFT JOIN pg_proc p ON p.oid=to_regprocedure(expected.signature)),
 'upstreamExactFunctions',(SELECT count(*)=12 FROM pg_proc p WHERE p.pronamespace='control_plane'::regnamespace
   AND p.proname IN(SELECT split_part(split_part(signature,'.',2),'(',1) FROM upstream_expected)),
 'upstreamColumns',(SELECT count(*)=2 AND bool_and(a.attnum IS NOT NULL AND a.atttypid=to_regtype(expected.type_name)
   AND a.attnotnull=expected.required AND a.atthasdef=expected.required
   AND (NOT expected.required OR pg_get_expr(d.adbin,d.adrelid)='''''::text'))
   FROM (VALUES('run_nodes','workflow_rework_gate_id','uuid',false),('owner_gates','closure_kind','text',true))
     expected(table_name,column_name,type_name,required)
   LEFT JOIN pg_attribute a ON a.attrelid=to_regclass('control_plane.'||expected.table_name)
     AND a.attname=expected.column_name AND a.attnum>0 AND NOT a.attisdropped
   LEFT JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum),
 'upstreamForeignKey',EXISTS(SELECT 1 FROM pg_constraint fk
   WHERE fk.conrelid='control_plane.run_nodes'::regclass AND fk.conname='workflow_rework_gate_fk'
     AND fk.contype='f' AND fk.confrelid='control_plane.owner_gates'::regclass
     AND fk.convalidated AND fk.conenforced AND fk.condeferrable AND NOT fk.condeferred
     AND fk.confmatchtype='s' AND fk.confdeltype='a' AND fk.confupdtype='a'
     AND fk.conkey=ARRAY[(SELECT attnum FROM pg_attribute WHERE attrelid=fk.conrelid AND attname='workflow_rework_gate_id')]::smallint[]
     AND fk.confkey=ARRAY[(SELECT attnum FROM pg_attribute WHERE attrelid=fk.confrelid AND attname='id')]::smallint[]),
 'upstreamConstraint',EXISTS(SELECT 1 FROM pg_constraint c WHERE c.conrelid='control_plane.owner_gates'::regclass
   AND c.conname='owner_gate_closure_kind' AND c.contype='c' AND c.convalidated AND c.conenforced AND NOT c.connoinherit
   AND regexp_replace(replace(pg_get_constraintdef(c.oid),'::text',''),'[[:space:]]','','g')=
     $expected$CHECK(((closure_kind='')OR((closure_kind='SUPERSEDED')AND(state='CANCELLED')AND(decisionISNULL))))$expected$)
 AND EXISTS(SELECT 1 FROM pg_constraint c WHERE c.conrelid='control_plane.owner_gates'::regclass
   AND c.conname='owner_gates_resolution_check' AND c.contype='c' AND c.convalidated AND c.conenforced AND NOT c.connoinherit
   AND regexp_replace(replace(pg_get_constraintdef(c.oid),'::text',''),'[[:space:]]','','g')=
     $expected$CHECK((((closure_kind='')AND(((state='OPEN')AND(decisionISNULL)AND(resolved_byISNULL)AND(resolved_atISNULL))OR((state='EXPIRED')AND(decisionISNULL)AND(resolved_byISNULL)AND(resolved_atISNOTNULL)AND(expires_atISNOTNULL)AND(integration_invocation_idISNULL))OR((state=ANY(ARRAY['APPROVED','REJECTED','CHANGES_REQUESTED','CANCELLED']))AND(decisionISNOTNULL)AND(resolved_byISNOTNULL)AND(resolved_atISNOTNULL))))OR((closure_kind='SUPERSEDED')AND(state='CANCELLED')AND(decisionISNULL)AND(resolved_byISNOTNULL)AND(resolved_atISNOTNULL)AND(integration_invocation_idISNULL))))$expected$),
 'upstreamIndex',EXISTS(SELECT 1 FROM pg_index i JOIN pg_class relation ON relation.oid=i.indexrelid
   JOIN pg_am am ON am.oid=relation.relam
   WHERE i.indexrelid=to_regclass('control_plane.workflow_rework_gate_pin') AND i.indrelid='control_plane.run_nodes'::regclass
     AND i.indisvalid AND i.indisready AND i.indislive AND NOT i.indisunique AND NOT i.indisprimary
     AND i.indnkeyatts=1 AND i.indnatts=1 AND i.indexprs IS NULL AND am.amname='btree'
     AND pg_get_indexdef(i.indexrelid,1,true)='workflow_rework_gate_id'
     AND NOT EXISTS(SELECT 1 FROM unnest(i.indoption) option WHERE option<>0)
     AND regexp_replace(pg_get_expr(i.indpred,i.indrelid),'[[:space:]]','','g')='(workflow_rework_gate_idISNOTNULL)'),
 'upstreamTriggers',(SELECT count(*)=2 AND bool_and(EXISTS(SELECT 1 FROM pg_trigger t
   WHERE t.tgrelid=to_regclass('control_plane.'||expected.table_name) AND t.tgname=expected.trigger_name
     AND t.tgfoid=to_regprocedure(expected.signature) AND t.tgtype=expected.kind AND t.tgenabled='O'
     AND t.tgattr=''::int2vector AND t.tgqual IS NULL AND t.tgnargs=0 AND NOT t.tgisinternal
     AND NOT t.tgdeferrable AND NOT t.tginitdeferred))
   FROM (VALUES('run_nodes','workflow_rework_node_guard','control_plane.guard_workflow_rework_node()',31),
     ('owner_gates','workflow_superseded_gate_guard','control_plane.guard_workflow_superseded_gate()',23))
     expected(table_name,trigger_name,signature,kind)),
 'upstreamColumnAccess',(SELECT count(*)=2 AND bool_and(
   has_column_privilege('control_plane_runtime','control_plane.'||expected.table_name,expected.column_name,'SELECT')
   AND has_column_privilege('control_plane_runtime','control_plane.'||expected.table_name,expected.column_name,'UPDATE'))
   FROM (VALUES('run_nodes','workflow_rework_gate_id'),('owner_gates','closure_kind')) expected(table_name,column_name))

)) AS proof FROM base_proof
), evidence_expected(signature,digest,language,volatility,result_type) AS (VALUES
 ('control_plane.guard_workflow_effect_records()','7010547e11bc8d1e8f67ac56feaba030','plpgsql','v','trigger'),
 ('control_plane.guard_workflow_attempt_journal()','7f8dc9469eb755a7b7760e8d4d7fc2cd','plpgsql','v','trigger'),
 ('control_plane.workflow_public_attempt_results(uuid,uuid)','80b6457cc872824d87164494cd44dc1a','plpgsql','s','jsonb'),
 ('control_plane.workflow_evidence_intent_valid(uuid,uuid,uuid,uuid,jsonb,jsonb)','9f92d166abb3267d2c3f1dc1a8e9ad1c','sql','s','boolean'),
 ('control_plane.workflow_evidence_outcome_valid(uuid,uuid,jsonb,jsonb)','c873f7e8247b0f54f0c0cf424ac7ba18','plpgsql','s','boolean'),
 ('control_plane.workflow_evidence_execution_assigned(uuid,uuid,uuid)','1fb462b1ccbdce5a35e6dcb497be0d40','sql','s','boolean'),
 ('control_plane.artifact_has_retained_revisions(uuid)','29166a148eef617208fc527afaf907d1','sql','s','boolean'),
 ('control_plane.workflow_evidence_retains_revision(uuid,uuid,uuid)','c347038ad84997023ea7f23b14fc527b','sql','s','boolean')
), evidence_functions AS (
 SELECT e.*,p.oid,CASE WHEN p.oid='control_plane.guard_workflow_effect_records()'::regprocedure THEN replace(p.prosrc,$combined_deployment$                    OR (requirement.value->>'Kind'='DEPLOYMENT' AND requirement.value->>'Operation'='ssh.compose.deploy'
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
                    $combined_deployment$,'') ELSE p.prosrc END AS prosrc,p.proowner,p.prosecdef,p.provolatile,p.prolang,p.proconfig,p.prorettype,p.proretset,p.proacl
 FROM evidence_expected e LEFT JOIN pg_proc p ON p.oid=to_regprocedure(e.signature)
), evidence_runtime_members(oid) AS (
 SELECT oid FROM pg_roles WHERE rolname='control_plane_runtime'
 UNION SELECT m.member FROM pg_auth_members m JOIN evidence_runtime_members parent ON parent.oid=m.roleid
), evidence_runtime_roles AS (
 SELECT r.* FROM pg_roles r JOIN evidence_runtime_members USING(oid)
), evidence_reachable AS (
 SELECT r.* FROM pg_roles r WHERE EXISTS(SELECT 1 FROM evidence_runtime_roles member WHERE pg_has_role(member.oid,r.oid,'SET'))
), evidence_triggers(table_name,trigger_name,signature,kind) AS (VALUES
 ('run_nodes','workflow_effect_records_guard','control_plane.guard_workflow_effect_records()',31),
 ('run_nodes','workflow_attempt_journal_guard','control_plane.guard_workflow_attempt_journal()',23)
), evidence_trigger_rows AS (
 SELECT e.*,t.oid,t.tgfoid,t.tgtype,t.tgenabled,t.tgattr,t.tgqual,t.tgnargs,t.tgisinternal,t.tgdeferrable
 FROM evidence_triggers e LEFT JOIN pg_trigger t ON t.tgrelid=to_regclass('control_plane.'||e.table_name) AND t.tgname=e.trigger_name
), evidence_nodes(relation_id) AS (
 SELECT 'control_plane.projects'::regclass::oid UNION
 SELECT c.conrelid FROM evidence_nodes n JOIN pg_constraint c ON c.confrelid=n.relation_id
 WHERE c.contype='f' AND c.connamespace='control_plane'::regnamespace AND c.conrelid NOT IN ('control_plane.session_workspace_storage'::regclass,'control_plane.session_workspace_volumes'::regclass,'control_plane.session_workspace_volume_tasks'::regclass,'control_plane.session_workspace_volume_attempts'::regclass)
), evidence_edges AS (
 SELECT cn.nspname||'.'||cc.relname||'|'||c.conname||'|'||pn.nspname||'.'||pc.relname||'|'||c.confdeltype::text AS pin,
 cn.nspname AS child_schema,cc.relname AS child_name,c.conname
 FROM pg_constraint c JOIN pg_class cc ON cc.oid=c.conrelid JOIN pg_namespace cn ON cn.oid=cc.relnamespace
 JOIN pg_class pc ON pc.oid=c.confrelid JOIN pg_namespace pn ON pn.oid=pc.relnamespace
 WHERE c.contype='f' AND c.confrelid IN(SELECT relation_id FROM evidence_nodes) AND c.conrelid IN(SELECT relation_id FROM evidence_nodes)
), evidence_edge_proof AS (
 SELECT count(*) AS count,md5(string_agg(pin,E'\n' ORDER BY child_schema,child_name,conname)) AS digest FROM evidence_edges
), evidence_node_proof AS (
 SELECT count(*) AS count,md5(string_agg(n.nspname||'.'||r.relname,E'\n' ORDER BY n.nspname,r.relname)) AS digest
 FROM evidence_nodes JOIN pg_class r ON r.oid=evidence_nodes.relation_id JOIN pg_namespace n ON n.oid=r.relnamespace
), evidence_purge_guard AS (
 SELECT * FROM pg_proc WHERE oid='control_plane.purge_project_database(uuid,uuid,text)'::regprocedure
), evidence_proof AS (
SELECT jsonb_build_object(
 'readOnly',current_setting('transaction_read_only'),'database',current_database(),
 'gooseVersion',(SELECT max(version_id) FILTER(WHERE is_applied) FROM public.goose_db_version WHERE version_id<20261026001500),
 'reviewQAEvidenceProfile','GITHUB_REVIEW_QA_V1',
 'reviewQAEvidenceSchemaOwner',(SELECT nspowner='control_plane_owner'::regrole FROM pg_namespace WHERE oid='control_plane'::regnamespace),
 'reviewQAEvidenceFunctionCount',(SELECT count(oid) FROM evidence_functions),
 'reviewQAEvidenceExactFunctions',(SELECT count(*)=8 FROM pg_proc WHERE pronamespace='control_plane'::regnamespace AND proname IN ('guard_workflow_effect_records','guard_workflow_attempt_journal','workflow_public_attempt_results','artifact_has_retained_revisions','workflow_evidence_intent_valid','workflow_evidence_outcome_valid','workflow_evidence_execution_assigned','workflow_evidence_retains_revision')),
 'reviewQAEvidenceFunctionSources',(SELECT count(*)=8 AND bool_and(oid IS NOT NULL AND md5(prosrc)=digest) FROM evidence_functions),
 'reviewQAEvidenceFunctionMetadata',(SELECT count(*)=8 AND bool_and(oid IS NOT NULL AND proowner='control_plane_owner'::regrole
    AND prosecdef=(signature='control_plane.artifact_has_retained_revisions(uuid)') AND provolatile::text=volatility AND prolang=(SELECT oid FROM pg_language WHERE lanname=language)
    AND cardinality(proconfig)=1 AND regexp_replace(proconfig[1],'[[:space:]]','','g')=CASE WHEN signature IN ('control_plane.artifact_has_retained_revisions(uuid)','control_plane.workflow_evidence_retains_revision(uuid,uuid,uuid)') THEN 'search_path=pg_catalog' ELSE 'search_path=pg_catalog,control_plane' END
    AND prorettype=to_regtype(result_type) AND NOT proretset) FROM evidence_functions),
 'reviewQAEvidenceFunctionACL',(SELECT count(*)=8 AND bool_and(oid IS NOT NULL AND has_function_privilege('control_plane_runtime',oid,'EXECUTE')=(signature<>'control_plane.workflow_evidence_retains_revision(uuid,uuid,uuid)')
    AND (signature<>'control_plane.artifact_has_retained_revisions(uuid)' OR has_function_privilege('artifact_retention_runtime',oid,'EXECUTE'))
    AND (signature<>'control_plane.workflow_evidence_retains_revision(uuid,uuid,uuid)' OR NOT EXISTS(
        SELECT 1 FROM aclexplode(coalesce(proacl,acldefault('f',proowner))) a WHERE a.grantee<>proowner AND a.privilege_type='EXECUTE'))
    AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(proacl,acldefault('f',proowner))) a
      WHERE (a.grantee=0 AND a.privilege_type='EXECUTE') OR (a.grantee<>'control_plane_owner'::regrole AND a.is_grantable))) FROM evidence_functions),
 'reviewQAEvidenceRuntimeRolesUnprivileged',EXISTS(SELECT 1 FROM evidence_runtime_roles WHERE rolcanlogin)
    AND NOT EXISTS(SELECT 1 FROM evidence_reachable WHERE rolsuper OR rolcreaterole OR rolcreatedb OR rolbypassrls)
    AND NOT EXISTS(SELECT 1 FROM evidence_runtime_roles WHERE pg_has_role(oid,'control_plane_owner','SET')),
 'reviewQAEvidenceTriggers',(SELECT count(*)=2 AND bool_and(oid IS NOT NULL AND tgfoid=to_regprocedure(signature) AND tgtype=kind
    AND tgenabled='O' AND tgattr=''::int2vector AND tgqual IS NULL AND tgnargs=0 AND NOT tgisinternal AND NOT tgdeferrable) FROM evidence_trigger_rows),
 'purgeNodes',(SELECT count FROM evidence_node_proof),'purgeNodeFingerprint',(SELECT digest FROM evidence_node_proof),
 'purgeEdges',(SELECT count FROM evidence_edge_proof),'purgeEdgeFingerprint',(SELECT digest FROM evidence_edge_proof),
 'reviewQAEvidencePurgeGuard',EXISTS(SELECT 1 FROM evidence_purge_guard p WHERE p.proowner='control_plane_owner'::regrole AND p.prosecdef
    AND p.provolatile='v' AND p.prorettype='integer'::regtype AND NOT p.proretset
    AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql') AND p.proconfig=ARRAY['search_path=pg_catalog']
    AND md5(replace(replace(replace(p.prosrc,(SELECT nodes FROM workspace_guards),'IF v_nodes <> 105 OR v_node_fingerprint <> ''f51a0de25bd942a250997775617eeaee'' THEN'),(SELECT edges FROM workspace_guards),format('IF v_edges <> 287 OR v_edge_fingerprint <> %L THEN',(SELECT digest FROM evidence_edge_proof))),format('IF v_edges <> 287 OR v_edge_fingerprint <> %L THEN',(SELECT digest FROM evidence_edge_proof)),
      'IF v_edges <> 284 OR v_edge_fingerprint <> ''6bf6ad76d8c7ae66e07f75ecf719fdd6'' THEN'))='b886c24d4e2f5c4a0ec08123d99462b0'
    AND has_function_privilege('control_plane_runtime',p.oid,'EXECUTE')
    AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE'))
) AS proof
)
SELECT jsonb_build_object(
 'profile','GITHUB_PDLC_PREDECESSOR_V1',
 'workflowPDLCAbsent',NOT EXISTS(SELECT 1 FROM pg_proc WHERE pronamespace='control_plane'::regnamespace AND proname IN('workflow_pdlc_file_key','workflow_pdlc_file_desired_digest','workflow_pdlc_file_epoch_digest','workflow_pdlc_result_digest','workflow_evidence_delivery_operation','workflow_pdlc_author_slot','workflow_pdlc_intent_valid','workflow_receipt_effect_proof','workflow_receipt_lineage_proof','workflow_native_receipt_proof','workflow_pdlc_read_order','workflow_pdlc_receipt_fact','workflow_pdlc_result_valid','workflow_pdlc_outcome_valid')),
 'database',current_database(),
 'oldClaimSessions', (SELECT count(*) FROM pg_stat_activity
        WHERE datname = current_database() AND pid <> pg_backend_pid()
            AND query LIKE '%WITH candidates AS%'
            AND query LIKE '%UPDATE control_plane.outbox_events e SET state=%'),
    'activeRuntimeRuns', (SELECT count(*) FROM control_plane.runs WHERE state IN ('QUEUED', 'RUNNING', 'CANCELLING', 'WAITING_HUMAN')),
    'activeRunNodes', (SELECT count(*) FROM control_plane.run_nodes WHERE state IN ('QUEUED', 'RUNNING', 'WAITING')),
    'activeSessionTurns', (SELECT count(*) FROM control_plane.session_turns WHERE state IN ('QUEUED', 'RUNNING')),
    'claimedRuntimeLeases', (SELECT count(*) FROM control_plane.runtime_leases WHERE state = 'CLAIMED'),
    'openBuilds', (SELECT count(*) FROM control_plane.image_builds WHERE stage NOT IN ('COMPLETED', 'FAILED', 'CANCELLED', 'EXPIRED', 'DEAD_LETTER')),
    'pendingAdmissions', (SELECT count(*) FROM control_plane.image_artifacts WHERE admission_state IN ('PENDING', 'CLAIMED')),
    'pendingPromotions', (SELECT count(*) FROM control_plane.role_image_promotion_requests WHERE state IN ('QUEUED', 'PROMOTING')) +
        (SELECT count(*) FROM control_plane.image_artifacts artifact
            WHERE artifact.promotion_state IN ('CLAIMED', 'AUTHORIZED') AND NOT EXISTS (
                SELECT 1 FROM control_plane.role_image_promotion_requests request
                WHERE request.id = artifact.promotion_request_id AND request.image_artifact_id = artifact.id
                    AND request.organization_id = artifact.organization_id
                    AND request.project_id IS NOT DISTINCT FROM artifact.project_id
                    AND request.state IN ('QUEUED', 'PROMOTING'))),
    'activeArchiveTasks', (SELECT count(*) FROM control_plane.session_archive_tasks WHERE state IN ('READY', 'CLAIMED')),
    'activeModelCatalogTasks', (SELECT count(*) FROM control_plane.provider_model_catalog_tasks WHERE state IN ('PENDING', 'CLAIMED')),
    'activeRenewalTasks', (SELECT count(*) FROM control_plane.provider_credential_renewal_tasks WHERE state IN ('CLAIMED', 'AUTHORIZED', 'UNKNOWN_OUTCOME')),
 'githubAutonomousWrites',EXISTS(SELECT 1 FROM pg_constraint c WHERE c.conrelid='control_plane.integration_invocations'::regclass
 AND c.conname='integration_invocations_approval_check' AND c.contype='c' AND c.convalidated AND c.conislocal AND NOT c.connoinherit
 AND md5(replace(pg_get_constraintdef(c.oid),'ARRAY[''github.issue.comment.create''::text, ''github.issue.comment.update''::text, ''github.pull_request.create''::text, ''github.pull_request.update''::text, ''github.pull_request.review.create''::text, ''github.issue.create''::text, ''github.issue.update''::text, ''github.repository.content.create''::text, ''github.repository.content.update''::text, ''github.pull_request.comment.create''::text, ''github.pull_request.comment.update''::text, ''github.pull_request.ready''::text]','ARRAY[''github.issue.comment.create''::text, ''github.issue.comment.update''::text, ''github.pull_request.create''::text, ''github.pull_request.update''::text, ''github.pull_request.review.create''::text]') )='87f6bc2dd7ef67bf3685d20f753fb474' AND (length(pg_get_constraintdef(c.oid))-length(replace(pg_get_constraintdef(c.oid),'ARRAY[''github.issue.comment.create''::text, ''github.issue.comment.update''::text, ''github.pull_request.create''::text, ''github.pull_request.update''::text, ''github.pull_request.review.create''::text, ''github.issue.create''::text, ''github.issue.update''::text, ''github.repository.content.create''::text, ''github.repository.content.update''::text, ''github.pull_request.comment.create''::text, ''github.pull_request.comment.update''::text, ''github.pull_request.ready''::text]','')))/459=1 )
 AND EXISTS(SELECT 1 FROM pg_proc p WHERE p.oid='control_plane.protect_email_mailbox_gate()'::regprocedure
 AND md5(p.prosrc)='398dfe88d74ba308b4d398757bdafdf9' AND pg_get_userbyid(p.proowner)='control_plane_owner'
 AND NOT p.prosecdef AND p.provolatile='v' AND p.prorettype='trigger'::regtype
 AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql') AND p.proconfig IS NULL
 AND has_function_privilege('control_plane_runtime',p.oid,'EXECUTE')
 AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a
 WHERE a.grantee NOT IN (0,'control_plane_owner'::regrole,'control_plane_runtime'::regrole) OR a.is_grantable OR a.privilege_type<>'EXECUTE'))
 AND EXISTS(SELECT 1 FROM pg_trigger t WHERE t.tgrelid='control_plane.integration_invocations'::regclass
 AND t.tgfoid='control_plane.protect_email_mailbox_gate()'::regprocedure AND t.tgenabled='O'
 AND md5(pg_get_triggerdef(t.oid))='b99f51573b8e4a909423c4bd058b1002'),
 'githubPRCatalog',EXISTS(SELECT 1 FROM pg_proc p WHERE p.oid='control_plane.integration_github_legacy_capabilities(text,text,text,text)'::regprocedure
 AND md5(p.prosrc)='ca35090d3f604db55ff1b753f5244c02' AND pg_get_userbyid(p.proowner)='control_plane_owner'
 AND NOT p.prosecdef AND p.provolatile='i' AND p.prorettype='jsonb'::regtype
 AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='sql')
 AND p.proconfig=ARRAY['search_path=pg_catalog, control_plane']
 AND has_function_privilege('control_plane_runtime',p.oid,'EXECUTE')
 AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a
 WHERE a.grantee NOT IN ('control_plane_owner'::regrole,'control_plane_runtime'::regrole)
 OR a.grantee='control_plane_runtime'::regrole AND (a.is_grantable OR a.privilege_type<>'EXECUTE')) AND p.proargnames=ARRAY['p_current_version','p_current_digest','p_version','p_digest']) AND EXISTS(SELECT 1 FROM pg_proc p WHERE p.oid='control_plane.integration_grant_admission(uuid,uuid,uuid,text,text,text,text,text,text,text[])'::regprocedure
 AND md5(p.prosrc)='e756ed73c355af684c20576f1acc7abd' AND pg_get_userbyid(p.proowner)='control_plane_owner'
 AND NOT p.prosecdef AND p.provolatile='s' AND p.prorettype='record'::regtype
 AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='sql')
 AND p.proconfig=ARRAY['search_path=pg_catalog, control_plane']
 AND has_function_privilege('control_plane_runtime',p.oid,'EXECUTE')
 AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a
 WHERE a.grantee NOT IN ('control_plane_owner'::regrole,'control_plane_runtime'::regrole)
 OR a.grantee='control_plane_runtime'::regrole AND (a.is_grantable OR a.privilege_type<>'EXECUTE'))),
 'readOnly',current_setting('transaction_read_only'),
 'gooseVersion',(SELECT max(version_id) FILTER(WHERE is_applied) FROM public.goose_db_version),
 'workspaceCatalog',true,
 'volumeCatalog',true,
 'restoreCatalog',true,
 'sshComposeProfile','SSH_COMPOSE_V1',
 'sshColumns',(SELECT count(*)=7 AND bool_and(attnum IS NOT NULL AND atttypid=to_regtype(type_name) AND attnotnull=required
   AND atthasdef=(expected_default IS NOT NULL) AND actual_default IS NOT DISTINCT FROM expected_default AND owner='control_plane_owner') FROM ssh_column_rows),
 'sshConstraints',(SELECT count(*)=7 AND bool_and(oid IS NOT NULL AND convalidated AND conenforced AND NOT connoinherit AND actual_definition=definition) FROM ssh_check_rows),
 'sshFunctions',(SELECT count(*)=3 AND bool_and(oid IS NOT NULL AND md5(prosrc)=source_md5 AND pg_get_userbyid(proowner)='control_plane_owner'
   AND NOT prosecdef AND provolatile='v' AND prorettype='trigger'::regtype AND NOT proretset AND prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql')
   AND proconfig=ARRAY['search_path=pg_catalog'] AND has_function_privilege('control_plane_runtime',oid,'EXECUTE')
   AND NOT EXISTS(SELECT 1 FROM aclexplode(COALESCE(proacl,acldefault('f',proowner))) acl WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE')) FROM ssh_function_rows),
 'sshTriggers',(SELECT count(*)=4 AND bool_and(oid IS NOT NULL AND NOT tgisinternal AND tgtype=19 AND tgenabled='O'
   AND tgattr=''::int2vector AND tgqual IS NULL AND tgnargs=0 AND NOT tgdeferrable AND tgfoid=to_regprocedure(signature)) FROM ssh_trigger_rows),
 'sshRuntimeColumns',(SELECT count(*)=7 AND bool_and(has_column_privilege('control_plane_runtime',to_regclass('control_plane.'||table_name),column_name,'SELECT')
   AND has_column_privilege('control_plane_runtime',to_regclass('control_plane.'||table_name),column_name,'UPDATE')) FROM ssh_columns),

 'checkpointCatalog',true,
 'purgeNodes',(SELECT count FROM workspace_node_proof),
 'purgeEdges',(SELECT count FROM workspace_edge_proof),
 'purgeNodeFingerprint',(SELECT digest FROM workspace_node_proof),
 'purgeEdgeFingerprint',(SELECT digest FROM workspace_edge_proof),
 'workspacePurgeGuard',EXISTS(SELECT 1 FROM pg_proc p CROSS JOIN workspace_guards g
   WHERE p.oid='control_plane.purge_project_database(uuid,uuid,text)'::regprocedure
     AND regexp_count(p.prosrc,'IF v_nodes <>')=1 AND regexp_count(p.prosrc,'IF v_edges <>')=1
     AND position(g.nodes IN p.prosrc)>0 AND position(g.edges IN p.prosrc)>0),
 'predecessorProjection',predecessor_proof.proof || evidence_proof.proof
)::text FROM predecessor_proof CROSS JOIN evidence_proof;
ROLLBACK;
