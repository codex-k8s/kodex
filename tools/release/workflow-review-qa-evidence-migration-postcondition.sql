-- name: workflow_review_qa_evidence_migration_postcondition :one
-- Инкрементальный закрытый proof014: predecessor013 проверяется отдельно своим
-- release-профилем. Здесь нет выдачи полномочий и нет чтения бизнесовых payload.
BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout='5s';
SET LOCAL lock_timeout='500ms';
WITH RECURSIVE expected(signature,digest,language,volatility,result_type) AS (VALUES
 ('control_plane.guard_workflow_effect_records()','7010547e11bc8d1e8f67ac56feaba030','plpgsql','v','trigger'),
 ('control_plane.guard_workflow_attempt_journal()','7f8dc9469eb755a7b7760e8d4d7fc2cd','plpgsql','v','trigger'),
 ('control_plane.workflow_public_attempt_results(uuid,uuid)','80b6457cc872824d87164494cd44dc1a','plpgsql','s','jsonb'),
 ('control_plane.workflow_evidence_intent_valid(uuid,uuid,uuid,uuid,jsonb,jsonb)','d37208f0d56739185b01fa5bc8fa6cbe','sql','s','boolean'),
 ('control_plane.workflow_evidence_outcome_valid(uuid,uuid,jsonb,jsonb)','4e03633b768583ef8cfd94c5243e21e0','plpgsql','s','boolean'),
 ('control_plane.workflow_evidence_execution_assigned(uuid,uuid,uuid)','1fb462b1ccbdce5a35e6dcb497be0d40','sql','s','boolean'),
 ('control_plane.artifact_has_retained_revisions(uuid)','29166a148eef617208fc527afaf907d1','sql','s','boolean'),
 ('control_plane.workflow_evidence_retains_revision(uuid,uuid,uuid)','bf04b902bc6ca9e2f513a3d192f65c25','sql','s','boolean')
), functions AS (
 SELECT e.*,p.oid,p.prosrc,p.proowner,p.prosecdef,p.provolatile,p.prolang,p.proconfig,p.prorettype,p.proretset,p.proacl
 FROM expected e LEFT JOIN pg_proc p ON p.oid=to_regprocedure(e.signature)
), runtime_members(oid) AS (
 SELECT oid FROM pg_roles WHERE rolname='control_plane_runtime'
 UNION SELECT m.member FROM pg_auth_members m JOIN runtime_members parent ON parent.oid=m.roleid
), runtime_roles AS (
 SELECT r.* FROM pg_roles r JOIN runtime_members USING(oid)
), reachable AS (
 SELECT r.* FROM pg_roles r WHERE EXISTS(SELECT 1 FROM runtime_roles member WHERE pg_has_role(member.oid,r.oid,'SET'))
), triggers(table_name,trigger_name,signature,kind) AS (VALUES
 ('run_nodes','workflow_effect_records_guard','control_plane.guard_workflow_effect_records()',31),
 ('run_nodes','workflow_attempt_journal_guard','control_plane.guard_workflow_attempt_journal()',23)
), trigger_rows AS (
 SELECT e.*,t.oid,t.tgfoid,t.tgtype,t.tgenabled,t.tgattr,t.tgqual,t.tgnargs,t.tgisinternal,t.tgdeferrable
 FROM triggers e LEFT JOIN pg_trigger t ON t.tgrelid=to_regclass('control_plane.'||e.table_name) AND t.tgname=e.trigger_name
), nodes(relation_id) AS (
 SELECT 'control_plane.projects'::regclass::oid UNION
 SELECT c.conrelid FROM nodes n JOIN pg_constraint c ON c.confrelid=n.relation_id
 WHERE c.contype='f' AND c.connamespace='control_plane'::regnamespace
), edges AS (
 SELECT cn.nspname||'.'||cc.relname||'|'||c.conname||'|'||pn.nspname||'.'||pc.relname||'|'||c.confdeltype::text AS pin,
 cn.nspname AS child_schema,cc.relname AS child_name,c.conname
 FROM pg_constraint c JOIN pg_class cc ON cc.oid=c.conrelid JOIN pg_namespace cn ON cn.oid=cc.relnamespace
 JOIN pg_class pc ON pc.oid=c.confrelid JOIN pg_namespace pn ON pn.oid=pc.relnamespace
 WHERE c.contype='f' AND c.confrelid IN(SELECT relation_id FROM nodes) AND c.conrelid IN(SELECT relation_id FROM nodes)
), edge_proof AS (
 SELECT count(*) AS count,md5(string_agg(pin,E'\n' ORDER BY child_schema,child_name,conname)) AS digest FROM edges
), node_proof AS (
 SELECT count(*) AS count,md5(string_agg(n.nspname||'.'||r.relname,E'\n' ORDER BY n.nspname,r.relname)) AS digest
 FROM nodes JOIN pg_class r ON r.oid=nodes.relation_id JOIN pg_namespace n ON n.oid=r.relnamespace
), purge_guard AS (
 SELECT * FROM pg_proc WHERE oid='control_plane.purge_project_database(uuid,uuid,text)'::regprocedure
)
SELECT jsonb_build_object(
 'readOnly',current_setting('transaction_read_only'),'database',current_database(),
 'gooseVersion',(SELECT max(version_id) FILTER(WHERE is_applied) FROM public.goose_db_version),
 'reviewQAEvidenceProfile','GITHUB_REVIEW_QA_V1',
 'schemaOwner',(SELECT nspowner='control_plane_owner'::regrole FROM pg_namespace WHERE oid='control_plane'::regnamespace),
 'functionCount',(SELECT count(oid) FROM functions),
 'functionSources',(SELECT count(*)=8 AND bool_and(oid IS NOT NULL AND md5(prosrc)=digest) FROM functions),
 'functionMetadata',(SELECT count(*)=8 AND bool_and(oid IS NOT NULL AND proowner='control_plane_owner'::regrole
    AND prosecdef=(signature='control_plane.artifact_has_retained_revisions(uuid)') AND provolatile::text=volatility AND prolang=(SELECT oid FROM pg_language WHERE lanname=language)
    AND cardinality(proconfig)=1 AND regexp_replace(proconfig[1],'[[:space:]]','','g')=CASE WHEN signature IN ('control_plane.artifact_has_retained_revisions(uuid)','control_plane.workflow_evidence_retains_revision(uuid,uuid,uuid)') THEN 'search_path=pg_catalog' ELSE 'search_path=pg_catalog,control_plane' END
    AND prorettype=to_regtype(result_type) AND NOT proretset) FROM functions),
 'functionACL',(SELECT count(*)=8 AND bool_and(oid IS NOT NULL AND has_function_privilege('control_plane_runtime',oid,'EXECUTE')=(signature<>'control_plane.workflow_evidence_retains_revision(uuid,uuid,uuid)')
    AND (signature<>'control_plane.artifact_has_retained_revisions(uuid)' OR has_function_privilege('artifact_retention_runtime',oid,'EXECUTE'))
    AND (signature<>'control_plane.workflow_evidence_retains_revision(uuid,uuid,uuid)' OR NOT EXISTS(
        SELECT 1 FROM aclexplode(coalesce(proacl,acldefault('f',proowner))) a WHERE a.grantee<>proowner AND a.privilege_type='EXECUTE'))
    AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(proacl,acldefault('f',proowner))) a
      WHERE (a.grantee=0 AND a.privilege_type='EXECUTE') OR (a.grantee<>'control_plane_owner'::regrole AND a.is_grantable))) FROM functions),
 'runtimeRolesUnprivileged',EXISTS(SELECT 1 FROM runtime_roles WHERE rolcanlogin)
    AND NOT EXISTS(SELECT 1 FROM reachable WHERE rolsuper OR rolcreaterole OR rolcreatedb OR rolbypassrls)
    AND NOT EXISTS(SELECT 1 FROM runtime_roles WHERE pg_has_role(oid,'control_plane_owner','SET')),
 'triggers',(SELECT count(*)=2 AND bool_and(oid IS NOT NULL AND tgfoid=to_regprocedure(signature) AND tgtype=kind
    AND tgenabled='O' AND tgattr=''::int2vector AND tgqual IS NULL AND tgnargs=0 AND NOT tgisinternal AND NOT tgdeferrable) FROM trigger_rows),
 'purgeNodes',(SELECT count FROM node_proof),'purgeNodeFingerprint',(SELECT digest FROM node_proof),
 'purgeEdges',(SELECT count FROM edge_proof),'purgeEdgeFingerprint',(SELECT digest FROM edge_proof),
 'purgeGuard',EXISTS(SELECT 1 FROM purge_guard p WHERE p.proowner='control_plane_owner'::regrole AND p.prosecdef
    AND p.provolatile='v' AND p.prorettype='integer'::regtype AND NOT p.proretset
    AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql') AND p.proconfig=ARRAY['search_path=pg_catalog']
    AND md5(replace(p.prosrc,format('IF v_edges <> 287 OR v_edge_fingerprint <> %L THEN',(SELECT digest FROM edge_proof)),
      'IF v_edges <> 284 OR v_edge_fingerprint <> ''6bf6ad76d8c7ae66e07f75ecf719fdd6'' THEN'))='b886c24d4e2f5c4a0ec08123d99462b0'
    AND has_function_privilege('control_plane_runtime',p.oid,'EXECUTE')
    AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE'))
)::text;
ROLLBACK;
