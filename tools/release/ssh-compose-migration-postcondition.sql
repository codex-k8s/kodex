-- name: ssh_compose_migration_postcondition :one
-- Только права и schema metadata; имена LOGIN, credentials и строки контекста не выдаются.
BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout = '5s';
SET LOCAL lock_timeout = '500ms';
WITH RECURSIVE runtime_members(oid) AS (
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
 ('control_plane.guard_workflow_effect_records()','00cd8f70bd3a1f6e8dc4505cbc6227c0','plpgsql','v','pg_catalog,control_plane'),
 ('control_plane.guard_workflow_physical_lineage()','1f08cfef012d3d1f8f7228dc1c17f2b8','plpgsql','v','pg_catalog,control_plane'),
 ('control_plane.guard_workflow_capture()','d354f3ce4d158d3b07ab806e0836edde','plpgsql','v','pg_catalog,control_plane'),
 ('control_plane.guard_workflow_attempt_journal()','2e03a8faa684d2c16ce8017fe8cef5e7','plpgsql','v','pg_catalog,control_plane'),
 ('control_plane.guard_workflow_input_snapshot()','2808dc6b5f79dc36e31b912aad24921b','plpgsql','v','pg_catalog,control_plane'),
 ('control_plane.guard_workflow_exception_episodes()','424c9bc2a9788805027ea226ad5b4acc','plpgsql','v','pg_catalog,control_plane'),
 ('control_plane.runtime_workflow_step(uuid,uuid)','40878ace3171345ad62227dae8e4eb71','sql','s','pg_catalog'),
 ('control_plane.workflow_captured_artifact_pins(uuid,uuid)','6dbed221a9e8882b4dc24eebc53a2d46','sql','s','pg_catalog'),
 ('control_plane.workflow_runtime_artifact_pins(uuid,uuid)','901a48c03a0f96b7e6ff3f529b406a9c','sql','s','pg_catalog'),
 ('control_plane.runtime_workflow_input_visible(uuid,uuid,uuid)','81167a271fc622d27d447e7110208e3f','sql','s','pg_catalog')
), function_rows AS (
 SELECT expected.*,p.oid,p.prosrc,p.proowner,p.prosecdef,p.proconfig,p.provolatile,p.prolang,p.prorettype,p.proretset,pg_get_function_result(p.oid) AS result_type,p.proacl
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
 WHERE c.contype='f' AND c.connamespace='control_plane'::regnamespace
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
 WHERE NOT(child_schema='control_plane' AND child_name='run_nodes' AND conname='workflow_physical_origin_fk')
), purge_guard AS (
 SELECT pg_get_functiondef('control_plane.purge_project_database(uuid,uuid,text)'::regprocedure) AS definition
), legacy_function_rows AS (
 SELECT p.* FROM pg_proc p WHERE p.oid IN (
   to_regprocedure('control_plane.runtime_file_source_visible(uuid,uuid,uuid,uuid,uuid,text,text,uuid)'),
   to_regprocedure('control_plane.artifact_has_retained_revisions(uuid)'))
), ssh_columns(table_name,column_name,type_name,required,expected_default) AS (VALUES
 ('runtime_secrets','purpose','text',true,'''APPLICATION''::text'),
 ('integration_invocations','ssh_compose_snapshot','jsonb',false,NULL),
 ('integration_invocations','ssh_compose_deadline','timestamp with time zone',false,NULL),
 ('integration_invocations','ssh_compose_recovery','boolean',true,'false'),
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
 ('integration_connection_tests','ssh_compose_test_snapshot_bounds',$expected$CHECK(((ssh_compose_snapshotISNULL)OR((jsonb_typeof(ssh_compose_snapshot)='object')AND(octet_length((ssh_compose_snapshot))<=32768))))$expected$)
), ssh_check_rows AS (
 SELECT e.*,c.oid,c.convalidated,c.conenforced,c.connoinherit,
 regexp_replace(replace(pg_get_constraintdef(c.oid),'::text',''),'[[:space:]]','','g') AS actual_definition
 FROM ssh_checks e LEFT JOIN pg_constraint c ON c.conrelid=to_regclass('control_plane.'||e.table_name) AND c.conname=e.constraint_name AND c.contype='c'
), ssh_functions(signature,source_md5) AS (VALUES
 ('control_plane.guard_ssh_compose_snapshot()','eb414956bfcd23b3e22d5093b6882e7d'),
 ('control_plane.guard_ssh_compose_secret_purpose()','da345f85ac1516dc7f2325bf43773c4f')
), ssh_function_rows AS (
 SELECT expected.*,p.* FROM ssh_functions expected LEFT JOIN pg_proc p ON p.oid=to_regprocedure(expected.signature)
), ssh_triggers(table_name,trigger_name,signature) AS (VALUES
 ('integration_invocations','ssh_compose_invocation_snapshot_immutable','control_plane.guard_ssh_compose_snapshot()'),
 ('integration_connection_tests','ssh_compose_test_snapshot_immutable','control_plane.guard_ssh_compose_snapshot()'),
 ('runtime_secrets','ssh_compose_secret_purpose_irreversible','control_plane.guard_ssh_compose_secret_purpose()')
), ssh_trigger_rows AS (
 SELECT expected.*,t.* FROM ssh_triggers expected LEFT JOIN pg_trigger t ON t.tgrelid=to_regclass('control_plane.'||expected.table_name) AND t.tgname=expected.trigger_name
)
SELECT jsonb_build_object(
    'readOnly',current_setting('transaction_read_only'),
    'database',current_database(),
    'gooseVersion',(SELECT max(version_id) FILTER (WHERE is_applied) FROM public.goose_db_version),
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

 'sshComposeProfile','SSH_COMPOSE_V1',
 'sshColumns',(SELECT count(*)=5 AND bool_and(attnum IS NOT NULL AND atttypid=to_regtype(type_name) AND attnotnull=required
   AND atthasdef=(expected_default IS NOT NULL) AND actual_default IS NOT DISTINCT FROM expected_default AND owner='control_plane_owner') FROM ssh_column_rows),
 'sshConstraints',(SELECT count(*)=6 AND bool_and(oid IS NOT NULL AND convalidated AND conenforced AND NOT connoinherit AND actual_definition=definition) FROM ssh_check_rows),
 'sshFunctions',(SELECT count(*)=2 AND bool_and(oid IS NOT NULL AND md5(prosrc)=source_md5 AND pg_get_userbyid(proowner)='control_plane_owner'
   AND NOT prosecdef AND provolatile='v' AND prorettype='trigger'::regtype AND NOT proretset AND prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql')
   AND proconfig=ARRAY['search_path=pg_catalog'] AND has_function_privilege('control_plane_runtime',oid,'EXECUTE')
   AND NOT EXISTS(SELECT 1 FROM aclexplode(COALESCE(proacl,acldefault('f',proowner))) acl WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE')) FROM ssh_function_rows),
 'sshTriggers',(SELECT count(*)=3 AND bool_and(oid IS NOT NULL AND NOT tgisinternal AND tgtype=19 AND tgenabled='O'
   AND tgattr=''::int2vector AND tgqual IS NULL AND tgnargs=0 AND NOT tgdeferrable AND tgfoid=to_regprocedure(signature)) FROM ssh_trigger_rows),
 'sshRuntimeColumns',(SELECT count(*)=5 AND bool_and(has_column_privilege('control_plane_runtime',to_regclass('control_plane.'||table_name),column_name,'SELECT')
   AND has_column_privilege('control_plane_runtime',to_regclass('control_plane.'||table_name),column_name,'UPDATE')) FROM ssh_columns),
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
   AND regexp_replace(replace(pg_get_expr(i.indpred,i.indrelid),'::text',''),'[[:space:]]','','g')=$expected$((workflow_step_key<>'')AND(workflow_step_origin_node_idISNULL))$expected$)
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
   AND (SELECT count=284 AND digest='6bf6ad76d8c7ae66e07f75ecf719fdd6' FROM edge_proof) AND (SELECT count=283 AND digest='be6e23d9e894687a69a19e9b5777ab09' FROM baseline_edges)
   AND (SELECT count(*)=1 FROM origin_fk)
   AND (SELECT regexp_count(definition,'IF v_nodes <> [0-9]+ OR v_node_fingerprint <> ''[a-f0-9]{32}'' THEN')=1
    AND regexp_count(definition,'IF v_edges <> [0-9]+ OR v_edge_fingerprint <> ''[a-f0-9]{32}'' THEN')=1
    AND position('IF v_nodes <> 105 OR v_node_fingerprint <> ''f51a0de25bd942a250997775617eeaee'' THEN' IN definition)>0
    AND position(format('IF v_edges <> 284 OR v_edge_fingerprint <> %L THEN',(SELECT digest FROM edge_proof)) IN definition)>0 FROM purge_guard)
,
 'visibilitySource',EXISTS(SELECT 1 FROM legacy_function_rows WHERE oid=to_regprocedure('control_plane.runtime_file_source_visible(uuid,uuid,uuid,uuid,uuid,text,text,uuid)')
 AND md5(prosrc)='be098f192205fa279eb55a502655052b'
 AND pg_get_userbyid(proowner)='control_plane_owner' AND NOT prosecdef AND provolatile='s'
 AND prolang=(SELECT oid FROM pg_language WHERE lanname='sql') AND prorettype='boolean'::regtype
 AND cardinality(proconfig)=1 AND regexp_replace(proconfig[1],'[[:space:]]','','g')='search_path=pg_catalog,control_plane'
 AND has_function_privilege('control_plane_runtime',oid,'EXECUTE')
 AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(proacl,acldefault('f',proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE')),
 'retentionSource',EXISTS(SELECT 1 FROM legacy_function_rows WHERE oid=to_regprocedure('control_plane.artifact_has_retained_revisions(uuid)')
 AND md5(prosrc)='1f71f6d19e09709f61f2c2273a9af0ca'
 AND pg_get_userbyid(proowner)='control_plane_owner' AND prosecdef AND provolatile='s'
 AND prolang=(SELECT oid FROM pg_language WHERE lanname='sql') AND prorettype='boolean'::regtype
 AND cardinality(proconfig)=1 AND regexp_replace(proconfig[1],'[[:space:]]','','g')='search_path=pg_catalog'
 AND has_function_privilege('control_plane_runtime',oid,'EXECUTE')
 AND has_function_privilege('artifact_retention_runtime',oid,'EXECUTE')
 AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(proacl,acldefault('f',proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE'))
)::text;
ROLLBACK;
