-- name: server_workflow_migration_precondition :one
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
), authority AS (
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
) AS proof
), new_columns(table_name,column_name) AS (VALUES
 ('run_nodes','workflow_step_origin_node_id'),
 ('run_nodes','workflow_business_attempt'),
 ('run_nodes','workflow_captured_artifacts'),
 ('run_nodes','workflow_attempts'),
 ('run_nodes','workflow_inputs'),
 ('run_nodes','workflow_effect_intents'),
 ('run_nodes','workflow_effect_rejoins'),
 ('runs','workflow_exception'),
 ('runs','workflow_exception_history')
), new_functions(signature) AS (VALUES
 ('control_plane.guard_workflow_effect_records()'),
 ('control_plane.guard_workflow_physical_lineage()'),
 ('control_plane.guard_workflow_capture()'),
 ('control_plane.guard_workflow_attempt_journal()'),
 ('control_plane.guard_workflow_input_snapshot()'),
 ('control_plane.guard_workflow_exception_episodes()'),
 ('control_plane.runtime_workflow_step(uuid,uuid)'),
 ('control_plane.workflow_captured_artifact_pins(uuid,uuid)'),
 ('control_plane.workflow_runtime_artifact_pins(uuid,uuid)'),
 ('control_plane.runtime_workflow_input_visible(uuid,uuid,uuid)')
), nodes(relation_id) AS (
 SELECT 'control_plane.projects'::regclass::oid UNION
 SELECT c.conrelid FROM nodes n JOIN pg_constraint c ON c.confrelid=n.relation_id
 WHERE c.contype='f' AND c.connamespace='control_plane'::regnamespace
), node_proof AS (
 SELECT count(*) AS count,md5(string_agg(ns.nspname||'.'||r.relname,E'\n' ORDER BY ns.nspname,r.relname)) AS digest
 FROM nodes JOIN pg_class r ON r.oid=nodes.relation_id JOIN pg_namespace ns ON ns.oid=r.relnamespace
), edge_proof AS (
 SELECT count(*) AS count,md5(string_agg(cn.nspname||'.'||cc.relname||'|'||c.conname||'|'||pn.nspname||'.'||pc.relname||'|'||c.confdeltype::text,
 E'\n' ORDER BY cn.nspname,cc.relname,c.conname)) AS digest
 FROM pg_constraint c JOIN pg_class cc ON cc.oid=c.conrelid JOIN pg_namespace cn ON cn.oid=cc.relnamespace
 JOIN pg_class pc ON pc.oid=c.confrelid JOIN pg_namespace pn ON pn.oid=pc.relnamespace
 WHERE c.contype='f' AND c.confrelid IN(SELECT relation_id FROM nodes) AND c.conrelid IN(SELECT relation_id FROM nodes)
), purge_guard AS (
 SELECT pg_get_functiondef('control_plane.purge_project_database(uuid,uuid,text)'::regprocedure) AS definition
)
SELECT jsonb_build_object(
    'readOnly', current_setting('transaction_read_only'),
    'database', current_database(),
    'gooseVersion', (SELECT max(version_id) FILTER (WHERE is_applied) FROM public.goose_db_version),
    'purgeAuthoritySafeToMigrate',
        (SELECT count(*)=2 AND bool_and(pg_get_userbyid(relowner)='control_plane_owner') FROM pg_class
         WHERE oid IN ('control_plane.project_purge_context'::regclass,'control_plane.project_purge_targets'::regclass))
        AND has_table_privilege('control_plane_runtime','control_plane.project_purge_context','SELECT')
        AND has_table_privilege('control_plane_runtime','control_plane.project_purge_targets','SELECT')
        AND NOT pg_has_role('control_plane_runtime','control_plane_owner','MEMBER')
        AND NOT EXISTS (SELECT 1 FROM control_plane.project_purge_context)
        AND NOT EXISTS (SELECT 1 FROM control_plane.project_purge_targets),
    'runtimeContextWriteBefore',has_table_privilege('control_plane_runtime','control_plane.project_purge_context','INSERT')
        OR has_table_privilege('control_plane_runtime','control_plane.project_purge_context','UPDATE')
        OR has_table_privilege('control_plane_runtime','control_plane.project_purge_context','DELETE'),
    'runtimeTargetsWriteBefore',has_table_privilege('control_plane_runtime','control_plane.project_purge_targets','INSERT')
        OR has_table_privilege('control_plane_runtime','control_plane.project_purge_targets','UPDATE')
        OR has_table_privilege('control_plane_runtime','control_plane.project_purge_targets','DELETE'),
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
    'activeRenewalTasks', (SELECT count(*) FROM control_plane.provider_credential_renewal_tasks WHERE state IN ('CLAIMED', 'AUTHORIZED', 'UNKNOWN_OUTCOME'))
,
 'serverWorkflowAbsent',
 NOT EXISTS(SELECT 1 FROM new_columns e JOIN pg_attribute a ON a.attrelid=to_regclass('control_plane.'||e.table_name)
   AND a.attname=e.column_name AND a.attnum>0 AND NOT a.attisdropped)
 AND NOT EXISTS(SELECT 1 FROM new_functions WHERE to_regprocedure(signature) IS NOT NULL)
 AND NOT EXISTS(SELECT 1 FROM pg_constraint WHERE connamespace='control_plane'::regnamespace
   AND conname IN('workflow_effect_record_bounds','workflow_physical_origin_fk','workflow_business_attempt_bound','workflow_capture_bound','workflow_journal_bounds','workflow_exception_history_bound'))
 AND NOT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid IN('control_plane.runs'::regclass,'control_plane.run_nodes'::regclass)
   AND tgname IN('workflow_effect_records_guard','workflow_physical_lineage_guard','workflow_capture_guard','workflow_attempt_journal_guard','workflow_input_snapshot_guard','workflow_exception_episodes_guard'))
 AND to_regclass('control_plane.run_nodes_workflow_physical_attempt') IS NULL
 AND (SELECT count(*)=2 AND bool_and(pg_get_userbyid(relowner)='control_plane_owner') FROM pg_class
   WHERE oid IN('control_plane.runs'::regclass,'control_plane.run_nodes'::regclass))
 AND EXISTS(SELECT 1 FROM pg_index i WHERE i.indexrelid=to_regclass('control_plane.run_nodes_workflow_step_once')
   AND i.indrelid='control_plane.run_nodes'::regclass AND i.indisvalid AND i.indisready AND i.indisunique
   AND i.indnatts=2 AND i.indnkeyatts=2 AND pg_get_indexdef(i.indexrelid,1,true)='root_run_id'
   AND pg_get_indexdef(i.indexrelid,2,true)='workflow_step_key'
   AND regexp_replace(replace(pg_get_expr(i.indpred,i.indrelid),'::text',''),'[[:space:]]','','g')=$expected$(workflow_step_key<>'')$expected$)
 AND (SELECT proof->'tables'='2'::jsonb AND (proof->>'runtimeRoles')::integer>=2
   AND (proof->>'runtimeLogins')::integer>=1 AND (proof->>'runtimeLogins')::integer<(proof->>'runtimeRoles')::integer
   AND NOT EXISTS(SELECT 1 FROM unnest(ARRAY['tableOwners','runtimeRolesUnprivileged','runtimeRead','runtimeMutationDenied','runtimeColumnMutationDenied','runtimeGrantOptionDenied','publicPrivilegesRevoked','ownerWrite','authorizationFunction','contextsEmpty']) field
     WHERE proof->field IS DISTINCT FROM 'true'::jsonb) FROM authority)
 AND EXISTS(SELECT 1 FROM pg_proc WHERE oid=to_regprocedure('control_plane.runtime_file_source_visible(uuid,uuid,uuid,uuid,uuid,text,text,uuid)')
   AND md5(prosrc)='e616be43adcd8e00f3f076514c3c56d5' AND pg_get_userbyid(proowner)='control_plane_owner'
   AND NOT prosecdef AND provolatile='s' AND prorettype='boolean'::regtype
   AND prolang=(SELECT oid FROM pg_language WHERE lanname='sql') AND cardinality(proconfig)=1
   AND regexp_replace(proconfig[1],'[[:space:]]','','g')='search_path=pg_catalog,control_plane'
   AND has_function_privilege('control_plane_runtime',oid,'EXECUTE')
   AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(proacl,acldefault('f',proowner))) acl WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE'))
 AND EXISTS(SELECT 1 FROM pg_proc WHERE oid=to_regprocedure('control_plane.artifact_has_retained_revisions(uuid)')
   AND md5(prosrc)='a47d0665a9c7bf837a82da01169a10f1' AND pg_get_userbyid(proowner)='control_plane_owner'
   AND prosecdef AND provolatile='s' AND prorettype='boolean'::regtype
   AND prolang=(SELECT oid FROM pg_language WHERE lanname='sql') AND cardinality(proconfig)=1
   AND regexp_replace(proconfig[1],'[[:space:]]','','g')='search_path=pg_catalog'
   AND has_function_privilege('control_plane_runtime',oid,'EXECUTE')
   AND has_function_privilege('artifact_retention_runtime',oid,'EXECUTE')
   AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(proacl,acldefault('f',proowner))) acl WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE'))
 AND (SELECT count=105 AND digest='f51a0de25bd942a250997775617eeaee' FROM node_proof)
 AND (SELECT count=283 AND digest='be6e23d9e894687a69a19e9b5777ab09' FROM edge_proof)
 AND (SELECT regexp_count(definition,'IF v_nodes <> [0-9]+ OR v_node_fingerprint <> ''[a-f0-9]{32}'' THEN')=1
   AND regexp_count(definition,'IF v_edges <> [0-9]+ OR v_edge_fingerprint <> ''[a-f0-9]{32}'' THEN')=1
   AND position('IF v_nodes <> 105 OR v_node_fingerprint <> ''f51a0de25bd942a250997775617eeaee'' THEN' IN definition)>0
   AND position('IF v_edges <> 283 OR v_edge_fingerprint <> ''be6e23d9e894687a69a19e9b5777ab09'' THEN' IN definition)>0 FROM purge_guard)
)::text;
ROLLBACK;
