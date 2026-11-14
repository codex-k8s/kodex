-- Generated: generate-telegram-notifications-proof.py; exact046 ACL-only restore.
SET LOCAL search_path=pg_catalog;SET LOCAL ROLE control_plane_owner;
SET LOCAL lock_timeout='8s';SET LOCAL statement_timeout='30s';
SELECT pg_advisory_xact_lock(hashtextextended('kodex.workspace.backup-acl-maintenance.v1',0));
DO $restore046$
DECLARE actual jsonb; activity jsonb;
BEGIN
 IF (-- Закрытая identity sequence канонического bootstrap. Public foreign sequences
-- не получают backup grants при GRANT ON ALL SEQUENCES. Table-only и canonical
-- допускают только отсутствие backup ACL либо точные SELECT+USAGE.
WITH registered AS (
 SELECT c.oid,c.relowner,c.relacl
 FROM pg_catalog.pg_class c
 JOIN pg_catalog.pg_sequence s ON s.seqrelid=c.oid
 WHERE c.relnamespace='public'::regnamespace AND c.relname='goose_db_version_id_seq'
 AND c.relkind='S' AND c.relowner='control_plane_owner'::regrole
 AND c.relpersistence='p' AND c.reloptions IS NULL
 AND s.seqtypid='integer'::regtype AND s.seqstart=1 AND s.seqincrement=1
 AND s.seqmin=1 AND s.seqmax=2147483647 AND s.seqcache=1 AND NOT s.seqcycle
 AND EXISTS(SELECT 1 FROM pg_catalog.pg_depend d JOIN pg_catalog.pg_attribute a
  ON a.attrelid=d.refobjid AND a.attnum=d.refobjsubid
  WHERE d.classid='pg_class'::regclass AND d.objid=c.oid AND d.objsubid=0
   AND d.refclassid='pg_class'::regclass AND d.refobjid='public.goose_db_version'::regclass
   AND d.deptype='i' AND a.attname='id' AND a.atttypid='integer'::regtype AND a.attidentity='d' AND NOT a.attisdropped)
), acl AS (
 SELECT a.* FROM registered r CROSS JOIN LATERAL pg_catalog.aclexplode(coalesce(r.relacl,pg_catalog.acldefault('S',r.relowner))) a
)
SELECT (SELECT count(*) FROM pg_catalog.pg_class WHERE relnamespace='public'::regnamespace AND relkind='S')=1
 AND (SELECT count(*) FROM registered)=1
 AND NOT EXISTS(SELECT 1 FROM acl WHERE grantor<>'control_plane_owner'::regrole OR is_grantable
  OR grantee NOT IN('control_plane_owner'::regrole,'control_plane_migrator'::regrole,'kodex_backup_reader'::regrole)
  OR grantee='kodex_backup_reader'::regrole AND privilege_type NOT IN('SELECT','USAGE'))
 AND (SELECT count(*) FROM acl WHERE grantee='control_plane_owner'::regrole AND privilege_type IN('SELECT','USAGE','UPDATE'))=3
 AND (SELECT count(*) FROM acl WHERE grantee='control_plane_migrator'::regrole AND privilege_type IN('SELECT','USAGE','UPDATE'))=3
 AND (SELECT count(*) FROM acl WHERE grantee='kodex_backup_reader'::regrole) IN(0,2)) IS DISTINCT FROM true THEN RAISE EXCEPTION 'Telegram notifications public sequence boundary changed';END IF;
IF (-- ACL-only maintenance не меняет workspace rows, filesystem, writer custody
-- или quiescence. Historical BLOCKED/PENDING records сохраняются без трактовки
-- как released. Закрывается только реально активный owner work/grant graph.
SELECT NOT EXISTS(SELECT 1 FROM control_plane.runs WHERE state NOT IN('SUCCEEDED','FAILED','CANCELLED'))
 AND NOT EXISTS(SELECT 1 FROM control_plane.run_nodes WHERE state IN('QUEUED','RUNNING','WAITING'))
 AND NOT EXISTS(SELECT 1 FROM control_plane.runtime_leases WHERE state='CLAIMED')
 AND NOT EXISTS(SELECT 1 FROM control_plane.session_archive_tasks WHERE state IN('READY','CLAIMED'))
 AND NOT EXISTS(SELECT 1 FROM control_plane.session_workspace_volume_tasks WHERE state IN('READY','CLAIMED'))
 -- Даже expired ACTIVE grant не считается CLOSED по времени.
 AND NOT EXISTS(SELECT 1 FROM control_plane.session_workspace_volume_attempts WHERE grant_state='ACTIVE')) IS DISTINCT FROM true THEN RAISE EXCEPTION 'Telegram notifications workspace owner is not idle';END IF;

 IF session_user<>'control_plane_migrator' OR current_database()<>'control_plane' OR (SELECT max(version_id) FROM public.goose_db_version WHERE is_applied)<>20261114140000 THEN RAISE EXCEPTION 'Telegram notifications backup restore boundary changed';END IF;
 IF EXISTS(SELECT 1 FROM pg_class WHERE relnamespace='public'::regnamespace AND relkind IN('r','p','v','m','f') AND oid<>'public.goose_db_version'::regclass)
 OR EXISTS(SELECT 1 FROM pg_class c CROSS JOIN LATERAL aclexplode(coalesce(c.relacl,acldefault(CASE WHEN c.relkind='S' THEN 'S'::"char" ELSE 'r'::"char" END,c.relowner))) a WHERE (c.relnamespace='control_plane'::regnamespace OR c.oid='public.goose_db_version'::regclass) AND a.grantee='kodex_backup_reader'::regrole AND ((a.privilege_type<>'SELECT' AND NOT(c.relkind='S' AND a.privilege_type='USAGE')) OR a.is_grantable OR a.grantor<>'control_plane_owner'::regrole)) THEN RAISE EXCEPTION 'Telegram notifications backup restore is not idle or safe';END IF;
 SELECT (-- Полный каталог; отдельно проверенные table SELECT/default row нормализованы.
-- Schema USAGE, sequence ACL и sequence default ACL сохраняются без нормализации.
-- name: github_repository_scope_catalog :one
-- Только schema metadata синтетического либо явно выбранного control-plane schema.
-- Business rows, LOGIN identities, credential descriptors и значения не читаются.
WITH RECURSIVE relations AS (
  SELECT c.* FROM pg_catalog.pg_class c
  WHERE (c.relnamespace='control_plane'::regnamespace OR c.oid='public.goose_db_version'::regclass) AND c.relkind IN ('r','p')

), functions AS (
  SELECT p.* FROM pg_catalog.pg_proc p
  WHERE p.pronamespace='control_plane'::regnamespace
), purge_nodes(id) AS (
  SELECT 'control_plane.projects'::regclass::oid UNION
  SELECT c.conrelid FROM purge_nodes n JOIN pg_catalog.pg_constraint c ON c.confrelid=n.id
  WHERE c.contype='f' AND c.connamespace='control_plane'::regnamespace
), predecessor_purge_nodes AS (
  SELECT id FROM purge_nodes WHERE id NOT IN
    ('control_plane.integration_personal_invocation_credentials'::regclass,
     'control_plane.integration_personal_invocation_admissions'::regclass)
), purge_node_metadata AS (
  SELECT r.oid AS id,n.nspname||'.'||r.relname AS name
  FROM purge_nodes selected JOIN pg_catalog.pg_class r ON r.oid=selected.id
  JOIN pg_catalog.pg_namespace n ON n.oid=r.relnamespace
), purge_edge_metadata AS (
  SELECT c.conrelid AS child_id,c.confrelid AS parent_id,cn.nspname||'.'||child.relname AS child,
    c.conname,pn.nspname||'.'||parent.relname AS parent,c.confdeltype::text AS deletion,
    cn.nspname||'.'||child.relname||'|'||c.conname||'|'||pn.nspname||'.'||parent.relname||'|'||c.confdeltype::text AS pin,
    jsonb_build_array(cn.nspname||'.'||child.relname,c.conname,pn.nspname||'.'||parent.relname,
      c.confdeltype,c.confupdtype,c.confmatchtype,c.condeferrable,c.condeferred,c.convalidated,c.conenforced,
      c.conislocal,c.coninhcount,pg_get_constraintdef(c.oid)) AS metadata
  FROM pg_catalog.pg_constraint c JOIN pg_catalog.pg_class child ON child.oid=c.conrelid
  JOIN pg_catalog.pg_namespace cn ON cn.oid=child.relnamespace
  JOIN pg_catalog.pg_class parent ON parent.oid=c.confrelid
  JOIN pg_catalog.pg_namespace pn ON pn.oid=parent.relnamespace
  WHERE c.contype='f' AND c.conrelid IN (SELECT id FROM purge_nodes)
    AND c.confrelid IN (SELECT id FROM purge_nodes)
), table_catalog AS (
  SELECT r.relname,jsonb_build_object(
    'kind',r.relkind,'persistence',r.relpersistence,'owner',pg_get_userbyid(r.relowner),
    'rls',r.relrowsecurity,'forceRls',r.relforcerowsecurity,'replicaIdentity',r.relreplident,
    'columns',(SELECT jsonb_agg(jsonb_build_array(a.attname,format_type(a.atttypid,a.atttypmod),a.attnotnull,
      a.atthasdef,pg_get_expr(d.adbin,d.adrelid),a.attidentity,a.attgenerated,a.attacl::text) ORDER BY a.attnum)
      FROM pg_catalog.pg_attribute a LEFT JOIN pg_catalog.pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum
      WHERE a.attrelid=r.oid AND a.attnum>0 AND NOT a.attisdropped),
    'constraints',(SELECT jsonb_object_agg(c.conname,jsonb_build_array(c.contype,c.convalidated,c.conenforced,
      c.condeferrable,c.condeferred,c.conislocal,c.coninhcount,c.connoinherit,pg_get_constraintdef(c.oid)))
      FROM pg_catalog.pg_constraint c WHERE c.conrelid=r.oid),
    'indexes',(SELECT jsonb_object_agg(i.indexrelid::regclass::text,jsonb_build_array(i.indisunique,i.indisvalid,
      i.indisready,i.indislive,pg_get_indexdef(i.indexrelid))) FROM pg_catalog.pg_index i WHERE i.indrelid=r.oid),
    'policies',(SELECT jsonb_object_agg(p.polname,jsonb_build_object('command',p.polcmd,'permissive',p.polpermissive,
      'roles',(SELECT jsonb_agg(CASE WHEN role_id=0 THEN 'PUBLIC' ELSE pg_get_userbyid(role_id) END ORDER BY role_id=0,pg_get_userbyid(role_id)) FROM unnest(p.polroles) role_id),
      'using',pg_get_expr(p.polqual,p.polrelid),'check',pg_get_expr(p.polwithcheck,p.polrelid)))
      FROM pg_catalog.pg_policy p WHERE p.polrelid=r.oid),
    'parents',(SELECT jsonb_agg(i.inhparent::regclass::text ORDER BY i.inhparent::regclass::text) FROM pg_catalog.pg_inherits i WHERE i.inhrelid=r.oid),
    'children',(SELECT jsonb_agg(i.inhrelid::regclass::text ORDER BY i.inhrelid::regclass::text) FROM pg_catalog.pg_inherits i WHERE i.inhparent=r.oid),
    'acl',(SELECT jsonb_agg(jsonb_build_array(CASE WHEN a.grantee=0 THEN 'PUBLIC' ELSE pg_get_userbyid(a.grantee) END,
      pg_get_userbyid(a.grantor),a.privilege_type,a.is_grantable) ORDER BY a.grantee=0,pg_get_userbyid(a.grantee),a.privilege_type)
      FROM aclexplode(coalesce(r.relacl,acldefault('r',r.relowner))) a WHERE a.grantee<>'kodex_backup_reader'::regrole)
  ) AS metadata FROM relations r
), function_catalog AS (
  SELECT p.oid::regprocedure::text AS name,jsonb_build_object(
    'definition',pg_get_functiondef(p.oid),'sourceMD5',md5(p.prosrc),'owner',pg_get_userbyid(p.proowner),'language',l.lanname,
    'definer',p.prosecdef,'kind',p.prokind,'volatility',p.provolatile,'parallel',p.proparallel,'strict',p.proisstrict,
    'leakproof',p.proleakproof,'set',p.proretset,'result',pg_get_function_result(p.oid),
    'argumentNames',p.proargnames,'argumentModes',p.proargmodes,'argumentTypes',pg_get_function_identity_arguments(p.oid),
    'defaults',p.pronargdefaults,'configuration',p.proconfig,
    'acl',(SELECT jsonb_agg(jsonb_build_array(CASE WHEN a.grantee=0 THEN 'PUBLIC' ELSE pg_get_userbyid(a.grantee) END,
      pg_get_userbyid(a.grantor),a.privilege_type,a.is_grantable) ORDER BY a.grantee=0,pg_get_userbyid(a.grantee),a.privilege_type)
      FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a)
  ) AS metadata FROM functions p JOIN pg_catalog.pg_language l ON l.oid=p.prolang
), trigger_catalog AS (
  SELECT r.relname||'.'||t.tgname AS name,jsonb_build_array(pg_get_triggerdef(t.oid),t.tgtype,t.tgenabled,
    t.tgdeferrable,t.tginitdeferred,t.tgconstraint<>0,encode(t.tgargs,'hex')) AS metadata
  FROM relations r JOIN pg_catalog.pg_trigger t ON t.tgrelid=r.oid WHERE NOT t.tgisinternal
)
, complete_catalog AS (
SELECT jsonb_build_object(
 'roles',(SELECT jsonb_object_agg(rolname,jsonb_build_array(rolsuper,rolinherit,rolcreaterole,rolcreatedb,rolcanlogin,rolreplication,rolbypassrls,rolconnlimit,rolconfig)) FROM pg_roles WHERE rolname IN('control_plane_owner','control_plane_runtime','control_plane_migrator','kodex_backup_reader')),
 'memberships',(SELECT jsonb_agg(jsonb_build_array(pg_get_userbyid(roleid),pg_get_userbyid(member),pg_get_userbyid(grantor),admin_option,inherit_option,set_option) ORDER BY pg_get_userbyid(roleid),pg_get_userbyid(member),pg_get_userbyid(grantor)) FROM pg_auth_members WHERE pg_get_userbyid(roleid) IN('control_plane_owner','control_plane_runtime','control_plane_migrator','kodex_backup_reader') OR pg_get_userbyid(member) IN('control_plane_owner','control_plane_runtime','control_plane_migrator','kodex_backup_reader')),
 'schema',(SELECT jsonb_build_array(pg_get_userbyid(nspowner),nspacl::text) FROM pg_namespace WHERE nspname='control_plane'),
 'defaultACL',(SELECT jsonb_agg(jsonb_build_array(pg_get_userbyid(defaclrole),CASE WHEN defaclnamespace=0 THEN '' ELSE defaclnamespace::regnamespace::text END,defaclobjtype,defaclacl::text) ORDER BY pg_get_userbyid(defaclrole),defaclnamespace::regnamespace::text,defaclobjtype) FROM pg_default_acl WHERE (defaclrole='control_plane_owner'::regrole OR defaclnamespace='control_plane'::regnamespace) AND NOT(defaclrole='control_plane_owner'::regrole AND defaclnamespace='control_plane'::regnamespace AND defaclobjtype='r')),
 'otherRelations',(SELECT jsonb_object_agg(c.relname,jsonb_build_array(c.relkind,pg_get_userbyid(c.relowner),(SELECT jsonb_agg(jsonb_build_array(CASE WHEN a.grantee=0 THEN 'PUBLIC' ELSE pg_get_userbyid(a.grantee) END,pg_get_userbyid(a.grantor),a.privilege_type,a.is_grantable) ORDER BY a.grantee=0,pg_get_userbyid(a.grantee),a.privilege_type) FROM aclexplode(coalesce(c.relacl,acldefault(CASE WHEN c.relkind='S' THEN 'S'::"char" ELSE 'r'::"char" END,c.relowner))) a WHERE c.relkind='S' OR a.grantee<>'kodex_backup_reader'::regrole),CASE WHEN c.relkind IN('v','m') THEN pg_get_viewdef(c.oid) ELSE NULL END,(SELECT jsonb_build_array(seqstart,seqincrement,seqmax,seqmin,seqcache,seqcycle) FROM pg_sequence WHERE seqrelid=c.oid))) FROM pg_class c WHERE c.relnamespace='control_plane'::regnamespace AND c.relkind NOT IN('r','p','i','I','t')),
 'types',(SELECT jsonb_object_agg(typname,jsonb_build_array(pg_get_userbyid(typowner),typtype,typcategory,typnotnull,typbasetype::regtype::text,typdefault)) FROM pg_type WHERE typnamespace='control_plane'::regnamespace),
 'tables',(SELECT jsonb_object_agg(relname,metadata) FROM table_catalog),
 'functions',(SELECT jsonb_object_agg(name,metadata) FROM function_catalog),
 'triggers',(SELECT jsonb_object_agg(name,metadata) FROM trigger_catalog),
 'purgeGraph',jsonb_build_object(
   'nodes',(SELECT jsonb_agg(name ORDER BY name) FROM purge_node_metadata),
   'edges',(SELECT jsonb_agg(metadata ORDER BY child,conname) FROM purge_edge_metadata),
   'nodeCount',(SELECT count(*) FROM purge_nodes),
   'edgeCount',(SELECT count(*) FROM purge_edge_metadata),
   'nodeFingerprint',(SELECT md5(string_agg(name,E'\n' ORDER BY name)) FROM purge_node_metadata),
   'edgeFingerprint',(SELECT md5(string_agg(pin,E'\n' ORDER BY child,conname)) FROM purge_edge_metadata),
   'predecessorNodeCount',(SELECT count(*) FROM predecessor_purge_nodes),
   'predecessorEdgeCount',(SELECT count(*) FROM purge_edge_metadata WHERE child_id IN (SELECT id FROM predecessor_purge_nodes) AND parent_id IN (SELECT id FROM predecessor_purge_nodes)),
   'predecessorNodeFingerprint',(SELECT md5(string_agg(name,E'\n' ORDER BY name)) FROM purge_node_metadata WHERE id IN (SELECT id FROM predecessor_purge_nodes)),
   'predecessorEdgeFingerprint',(SELECT md5(string_agg(pin,E'\n' ORDER BY child,conname)) FROM purge_edge_metadata WHERE child_id IN (SELECT id FROM predecessor_purge_nodes) AND parent_id IN (SELECT id FROM predecessor_purge_nodes))),
 'sequences',(SELECT jsonb_agg(c.relname ORDER BY c.relname) FROM pg_catalog.pg_class c
   WHERE c.relnamespace='control_plane'::regnamespace AND c.relkind='S' AND c.relname LIKE 'integration_personal_%')
) AS content
)
SELECT jsonb_build_object(
'tableDefaultACL',(SELECT jsonb_agg(defaclacl::text ORDER BY defaclacl::text) FROM pg_default_acl WHERE defaclrole='control_plane_owner'::regrole AND defaclnamespace='control_plane'::regnamespace AND defaclobjtype='r'),
'catalogDigest',encode(public.digest(convert_to(content::text,'UTF8'),'sha256'),'hex'),
'tablesDigest',encode(public.digest(convert_to((content->'tables')::text,'UTF8'),'sha256'),'hex'),
'functionsDigest',encode(public.digest(convert_to((content->'functions')::text,'UTF8'),'sha256'),'hex'),
'triggersDigest',encode(public.digest(convert_to((content->'triggers')::text,'UTF8'),'sha256'),'hex'),
'authorityDigest',encode(public.digest(convert_to((content-ARRAY['tables','functions','triggers','purgeGraph'])::text,'UTF8'),'sha256'),'hex'),
'tableCount',(SELECT count(*) FROM jsonb_object_keys(content->'tables')),
'functionCount',(SELECT count(*) FROM jsonb_object_keys(content->'functions')),
'triggerCount',(SELECT count(*) FROM jsonb_object_keys(content->'triggers')),
'purgeGraphDigest',encode(public.digest(convert_to((content->'purgeGraph')::text,'UTF8'),'sha256'),'hex'),
'purgeNodeCount',content#>'{purgeGraph,nodeCount}',
'purgeEdgeCount',content#>'{purgeGraph,edgeCount}',
'purgeNodeFingerprint',content#>'{purgeGraph,nodeFingerprint}',
'purgeEdgeFingerprint',content#>'{purgeGraph,edgeFingerprint}') FROM complete_catalog) INTO actual;
 IF (actual->>'catalogDigest' IS DISTINCT FROM 'fdca90bf1047fb06bfe92c8cec482758b9f14dc7ab57bd2cde6452304d1739e2' AND actual->>'catalogDigest' IS DISTINCT FROM '345bae3f900a1e7ccf37f792844b8031349f0157086a5cda5ae2a12b1fcc0131' AND actual->>'catalogDigest' IS DISTINCT FROM 'fdca90bf1047fb06bfe92c8cec482758b9f14dc7ab57bd2cde6452304d1739e2') OR (actual->'tableDefaultACL' IS DISTINCT FROM $normal$["{control_plane_runtime=arwd/control_plane_owner}"]$normal$::jsonb AND actual->'tableDefaultACL' IS DISTINCT FROM $backup$["{control_plane_runtime=arwd/control_plane_owner,kodex_backup_reader=r/control_plane_owner}"]$backup$::jsonb) THEN RAISE EXCEPTION 'Telegram notifications backup restore catalog changed';END IF;
 SELECT jsonb_build_object( 'activeRuntimeLeases',(SELECT count(*) FROM control_plane.runtime_leases WHERE state='CLAIMED'),
 'activeArchiveTasks',(SELECT count(*) FROM control_plane.session_archive_tasks WHERE state IN ('READY','CLAIMED')),
 'activeRuntimeRuns',(SELECT count(*) FROM control_plane.runs WHERE state NOT IN ('SUCCEEDED','FAILED','CANCELLED')),
 'activeRunNodes',(SELECT count(*) FROM control_plane.run_nodes WHERE state IN ('QUEUED','RUNNING','WAITING')),
 'activeSessionTurns',(SELECT count(*) FROM control_plane.session_turns WHERE state IN ('QUEUED','RUNNING')),
 'claimedRuntimeLeases',(SELECT count(*) FROM control_plane.runtime_leases WHERE state='CLAIMED'),
 'openBuilds',(SELECT count(*) FROM control_plane.image_builds WHERE stage NOT IN ('COMPLETED','FAILED','CANCELLED','EXPIRED','DEAD_LETTER')),
 'pendingAdmissions',(SELECT count(*) FROM control_plane.image_artifacts WHERE admission_state IN ('PENDING','CLAIMED')),
 'pendingPromotions',(SELECT count(*) FROM control_plane.role_image_promotion_requests WHERE state IN ('QUEUED','PROMOTING'))+
  (SELECT count(*) FROM control_plane.image_artifacts a WHERE a.promotion_state IN ('CLAIMED','AUTHORIZED')
   AND NOT EXISTS(SELECT 1 FROM control_plane.role_image_promotion_requests r WHERE r.id=a.promotion_request_id
    AND r.image_artifact_id=a.id AND r.organization_id=a.organization_id AND r.project_id IS NOT DISTINCT FROM a.project_id
    AND r.state IN ('QUEUED','PROMOTING'))),
 'activeModelCatalogTasks',(SELECT count(*) FROM control_plane.provider_model_catalog_tasks WHERE state IN ('PENDING','CLAIMED')),
 'activeRenewalTasks',(SELECT count(*) FROM control_plane.provider_credential_renewal_tasks WHERE state IN ('CLAIMED','AUTHORIZED','UNKNOWN_OUTCOME')),
 'oldClaimSessions',(SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid()
  AND query LIKE '%WITH candidates AS%' AND query LIKE '%UPDATE control_plane.outbox_events e SET state=%')
,
 'activeIntegrationInvocations',(SELECT count(*) FROM control_plane.integration_invocations WHERE state NOT IN ('SUCCEEDED','FAILED','CANCELLED','REJECTED','EXPIRED')),
 'activeIntegrationConnectionTests',(SELECT count(*) FROM control_plane.integration_connection_tests WHERE state IN ('DUE','CLAIMED')),
 'activePersonalCredentialTests',(SELECT count(*) FROM control_plane.integration_personal_credential_tests WHERE state IN ('DUE','CLAIMED','PROBING')),
 'activeRepositoryBundles',(SELECT count(*) FROM control_plane.repository_bundle_publications WHERE state IN ('WAITING_APPROVAL','QUEUED','CLAIMED','EFFECT_STARTED','UNKNOWN_OUTCOME')),
 'activeConfigurationSources',(SELECT count(*) FROM control_plane.managed_configuration_source_work WHERE state IN ('QUEUED','CLAIMED')),
 'activeConfigurationWritebacks',(SELECT count(*) FROM control_plane.managed_configuration_writebacks WHERE state IN ('WAITING_APPROVAL','QUEUED','CLAIMED','EFFECT_STARTED','UNKNOWN_OUTCOME')),
 'activeBackupSessions',(SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND usesysid IN (WITH RECURSIVE backup_roles(oid) AS (SELECT oid FROM pg_roles WHERE rolname='kodex_backup_reader' UNION SELECT m.member FROM pg_auth_members m JOIN backup_roles r ON m.roleid=r.oid) SELECT oid FROM backup_roles)),
 'activeInteractionDeliveries',(SELECT count(*) FROM control_plane.interaction_deliveries WHERE state IN ('WAITING_APPROVAL','DUE','CLAIMED') OR (state='FAILED' AND completed_at IS NULL))) INTO activity;
 IF EXISTS(SELECT 1 FROM jsonb_each_text(activity) WHERE value::bigint<>0) THEN RAISE EXCEPTION 'Telegram notifications backup owner is not idle';END IF;
 GRANT CONNECT ON DATABASE control_plane TO kodex_backup_reader;
GRANT USAGE ON SCHEMA public, control_plane TO kodex_backup_reader;
GRANT SELECT ON ALL TABLES IN SCHEMA public, control_plane TO kodex_backup_reader;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public, control_plane TO kodex_backup_reader;
ALTER DEFAULT PRIVILEGES FOR ROLE control_plane_owner IN SCHEMA control_plane
  GRANT SELECT ON TABLES TO kodex_backup_reader;
ALTER DEFAULT PRIVILEGES FOR ROLE control_plane_owner IN SCHEMA control_plane
  GRANT USAGE, SELECT ON SEQUENCES TO kodex_backup_reader;

 SELECT (-- name: github_repository_scope_catalog :one
-- Только schema metadata синтетического либо явно выбранного control-plane schema.
-- Business rows, LOGIN identities, credential descriptors и значения не читаются.
WITH RECURSIVE relations AS (
  SELECT c.* FROM pg_catalog.pg_class c
  WHERE (c.relnamespace='control_plane'::regnamespace OR c.oid='public.goose_db_version'::regclass) AND c.relkind IN ('r','p')

), functions AS (
  SELECT p.* FROM pg_catalog.pg_proc p
  WHERE p.pronamespace='control_plane'::regnamespace
), purge_nodes(id) AS (
  SELECT 'control_plane.projects'::regclass::oid UNION
  SELECT c.conrelid FROM purge_nodes n JOIN pg_catalog.pg_constraint c ON c.confrelid=n.id
  WHERE c.contype='f' AND c.connamespace='control_plane'::regnamespace
), predecessor_purge_nodes AS (
  SELECT id FROM purge_nodes WHERE id NOT IN
    ('control_plane.integration_personal_invocation_credentials'::regclass,
     'control_plane.integration_personal_invocation_admissions'::regclass)
), purge_node_metadata AS (
  SELECT r.oid AS id,n.nspname||'.'||r.relname AS name
  FROM purge_nodes selected JOIN pg_catalog.pg_class r ON r.oid=selected.id
  JOIN pg_catalog.pg_namespace n ON n.oid=r.relnamespace
), purge_edge_metadata AS (
  SELECT c.conrelid AS child_id,c.confrelid AS parent_id,cn.nspname||'.'||child.relname AS child,
    c.conname,pn.nspname||'.'||parent.relname AS parent,c.confdeltype::text AS deletion,
    cn.nspname||'.'||child.relname||'|'||c.conname||'|'||pn.nspname||'.'||parent.relname||'|'||c.confdeltype::text AS pin,
    jsonb_build_array(cn.nspname||'.'||child.relname,c.conname,pn.nspname||'.'||parent.relname,
      c.confdeltype,c.confupdtype,c.confmatchtype,c.condeferrable,c.condeferred,c.convalidated,c.conenforced,
      c.conislocal,c.coninhcount,pg_get_constraintdef(c.oid)) AS metadata
  FROM pg_catalog.pg_constraint c JOIN pg_catalog.pg_class child ON child.oid=c.conrelid
  JOIN pg_catalog.pg_namespace cn ON cn.oid=child.relnamespace
  JOIN pg_catalog.pg_class parent ON parent.oid=c.confrelid
  JOIN pg_catalog.pg_namespace pn ON pn.oid=parent.relnamespace
  WHERE c.contype='f' AND c.conrelid IN (SELECT id FROM purge_nodes)
    AND c.confrelid IN (SELECT id FROM purge_nodes)
), table_catalog AS (
  SELECT r.relname,jsonb_build_object(
    'kind',r.relkind,'persistence',r.relpersistence,'owner',pg_get_userbyid(r.relowner),
    'rls',r.relrowsecurity,'forceRls',r.relforcerowsecurity,'replicaIdentity',r.relreplident,
    'columns',(SELECT jsonb_agg(jsonb_build_array(a.attname,format_type(a.atttypid,a.atttypmod),a.attnotnull,
      a.atthasdef,pg_get_expr(d.adbin,d.adrelid),a.attidentity,a.attgenerated,a.attacl::text) ORDER BY a.attnum)
      FROM pg_catalog.pg_attribute a LEFT JOIN pg_catalog.pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum
      WHERE a.attrelid=r.oid AND a.attnum>0 AND NOT a.attisdropped),
    'constraints',(SELECT jsonb_object_agg(c.conname,jsonb_build_array(c.contype,c.convalidated,c.conenforced,
      c.condeferrable,c.condeferred,c.conislocal,c.coninhcount,c.connoinherit,pg_get_constraintdef(c.oid)))
      FROM pg_catalog.pg_constraint c WHERE c.conrelid=r.oid),
    'indexes',(SELECT jsonb_object_agg(i.indexrelid::regclass::text,jsonb_build_array(i.indisunique,i.indisvalid,
      i.indisready,i.indislive,pg_get_indexdef(i.indexrelid))) FROM pg_catalog.pg_index i WHERE i.indrelid=r.oid),
    'policies',(SELECT jsonb_object_agg(p.polname,jsonb_build_object('command',p.polcmd,'permissive',p.polpermissive,
      'roles',(SELECT jsonb_agg(CASE WHEN role_id=0 THEN 'PUBLIC' ELSE pg_get_userbyid(role_id) END ORDER BY role_id=0,pg_get_userbyid(role_id)) FROM unnest(p.polroles) role_id),
      'using',pg_get_expr(p.polqual,p.polrelid),'check',pg_get_expr(p.polwithcheck,p.polrelid)))
      FROM pg_catalog.pg_policy p WHERE p.polrelid=r.oid),
    'parents',(SELECT jsonb_agg(i.inhparent::regclass::text ORDER BY i.inhparent::regclass::text) FROM pg_catalog.pg_inherits i WHERE i.inhrelid=r.oid),
    'children',(SELECT jsonb_agg(i.inhrelid::regclass::text ORDER BY i.inhrelid::regclass::text) FROM pg_catalog.pg_inherits i WHERE i.inhparent=r.oid),
    'acl',(SELECT jsonb_agg(jsonb_build_array(CASE WHEN a.grantee=0 THEN 'PUBLIC' ELSE pg_get_userbyid(a.grantee) END,
      pg_get_userbyid(a.grantor),a.privilege_type,a.is_grantable) ORDER BY a.grantee=0,pg_get_userbyid(a.grantee),a.privilege_type)
      FROM aclexplode(coalesce(r.relacl,acldefault('r',r.relowner))) a)
  ) AS metadata FROM relations r
), function_catalog AS (
  SELECT p.oid::regprocedure::text AS name,jsonb_build_object(
    'definition',pg_get_functiondef(p.oid),'sourceMD5',md5(p.prosrc),'owner',pg_get_userbyid(p.proowner),'language',l.lanname,
    'definer',p.prosecdef,'kind',p.prokind,'volatility',p.provolatile,'parallel',p.proparallel,'strict',p.proisstrict,
    'leakproof',p.proleakproof,'set',p.proretset,'result',pg_get_function_result(p.oid),
    'argumentNames',p.proargnames,'argumentModes',p.proargmodes,'argumentTypes',pg_get_function_identity_arguments(p.oid),
    'defaults',p.pronargdefaults,'configuration',p.proconfig,
    'acl',(SELECT jsonb_agg(jsonb_build_array(CASE WHEN a.grantee=0 THEN 'PUBLIC' ELSE pg_get_userbyid(a.grantee) END,
      pg_get_userbyid(a.grantor),a.privilege_type,a.is_grantable) ORDER BY a.grantee=0,pg_get_userbyid(a.grantee),a.privilege_type)
      FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a)
  ) AS metadata FROM functions p JOIN pg_catalog.pg_language l ON l.oid=p.prolang
), trigger_catalog AS (
  SELECT r.relname||'.'||t.tgname AS name,jsonb_build_array(pg_get_triggerdef(t.oid),t.tgtype,t.tgenabled,
    t.tgdeferrable,t.tginitdeferred,t.tgconstraint<>0,encode(t.tgargs,'hex')) AS metadata
  FROM relations r JOIN pg_catalog.pg_trigger t ON t.tgrelid=r.oid WHERE NOT t.tgisinternal
)
, complete_catalog AS (
SELECT jsonb_build_object(
 'roles',(SELECT jsonb_object_agg(rolname,jsonb_build_array(rolsuper,rolinherit,rolcreaterole,rolcreatedb,rolcanlogin,rolreplication,rolbypassrls,rolconnlimit,rolconfig)) FROM pg_roles WHERE rolname IN('control_plane_owner','control_plane_runtime','control_plane_migrator','kodex_backup_reader')),
 'memberships',(SELECT jsonb_agg(jsonb_build_array(pg_get_userbyid(roleid),pg_get_userbyid(member),pg_get_userbyid(grantor),admin_option,inherit_option,set_option) ORDER BY pg_get_userbyid(roleid),pg_get_userbyid(member),pg_get_userbyid(grantor)) FROM pg_auth_members WHERE pg_get_userbyid(roleid) IN('control_plane_owner','control_plane_runtime','control_plane_migrator','kodex_backup_reader') OR pg_get_userbyid(member) IN('control_plane_owner','control_plane_runtime','control_plane_migrator','kodex_backup_reader')),
 'schema',(SELECT jsonb_build_array(pg_get_userbyid(nspowner),nspacl::text) FROM pg_namespace WHERE nspname='control_plane'),
 'defaultACL',(SELECT jsonb_agg(jsonb_build_array(pg_get_userbyid(defaclrole),CASE WHEN defaclnamespace=0 THEN '' ELSE defaclnamespace::regnamespace::text END,defaclobjtype,defaclacl::text) ORDER BY pg_get_userbyid(defaclrole),defaclnamespace::regnamespace::text,defaclobjtype) FROM pg_default_acl WHERE defaclrole='control_plane_owner'::regrole OR defaclnamespace='control_plane'::regnamespace),
 'otherRelations',(SELECT jsonb_object_agg(c.relname,jsonb_build_array(c.relkind,pg_get_userbyid(c.relowner),c.relacl::text,CASE WHEN c.relkind IN('v','m') THEN pg_get_viewdef(c.oid) ELSE NULL END,(SELECT jsonb_build_array(seqstart,seqincrement,seqmax,seqmin,seqcache,seqcycle) FROM pg_sequence WHERE seqrelid=c.oid))) FROM pg_class c WHERE c.relnamespace='control_plane'::regnamespace AND c.relkind NOT IN('r','p','i','I','t')),
 'types',(SELECT jsonb_object_agg(typname,jsonb_build_array(pg_get_userbyid(typowner),typtype,typcategory,typnotnull,typbasetype::regtype::text,typdefault)) FROM pg_type WHERE typnamespace='control_plane'::regnamespace),
 'tables',(SELECT jsonb_object_agg(relname,metadata) FROM table_catalog),
 'functions',(SELECT jsonb_object_agg(name,metadata) FROM function_catalog),
 'triggers',(SELECT jsonb_object_agg(name,metadata) FROM trigger_catalog),
 'purgeGraph',jsonb_build_object(
   'nodes',(SELECT jsonb_agg(name ORDER BY name) FROM purge_node_metadata),
   'edges',(SELECT jsonb_agg(metadata ORDER BY child,conname) FROM purge_edge_metadata),
   'nodeCount',(SELECT count(*) FROM purge_nodes),
   'edgeCount',(SELECT count(*) FROM purge_edge_metadata),
   'nodeFingerprint',(SELECT md5(string_agg(name,E'\n' ORDER BY name)) FROM purge_node_metadata),
   'edgeFingerprint',(SELECT md5(string_agg(pin,E'\n' ORDER BY child,conname)) FROM purge_edge_metadata),
   'predecessorNodeCount',(SELECT count(*) FROM predecessor_purge_nodes),
   'predecessorEdgeCount',(SELECT count(*) FROM purge_edge_metadata WHERE child_id IN (SELECT id FROM predecessor_purge_nodes) AND parent_id IN (SELECT id FROM predecessor_purge_nodes)),
   'predecessorNodeFingerprint',(SELECT md5(string_agg(name,E'\n' ORDER BY name)) FROM purge_node_metadata WHERE id IN (SELECT id FROM predecessor_purge_nodes)),
   'predecessorEdgeFingerprint',(SELECT md5(string_agg(pin,E'\n' ORDER BY child,conname)) FROM purge_edge_metadata WHERE child_id IN (SELECT id FROM predecessor_purge_nodes) AND parent_id IN (SELECT id FROM predecessor_purge_nodes))),
 'sequences',(SELECT jsonb_agg(c.relname ORDER BY c.relname) FROM pg_catalog.pg_class c
   WHERE c.relnamespace='control_plane'::regnamespace AND c.relkind='S' AND c.relname LIKE 'integration_personal_%')
) AS content
)
SELECT jsonb_build_object(
'tableDefaultACL',(SELECT jsonb_agg(defaclacl::text ORDER BY defaclacl::text) FROM pg_default_acl WHERE defaclrole='control_plane_owner'::regrole AND defaclnamespace='control_plane'::regnamespace AND defaclobjtype='r'),
'catalogDigest',encode(public.digest(convert_to(content::text,'UTF8'),'sha256'),'hex'),
'tablesDigest',encode(public.digest(convert_to((content->'tables')::text,'UTF8'),'sha256'),'hex'),
'functionsDigest',encode(public.digest(convert_to((content->'functions')::text,'UTF8'),'sha256'),'hex'),
'triggersDigest',encode(public.digest(convert_to((content->'triggers')::text,'UTF8'),'sha256'),'hex'),
'authorityDigest',encode(public.digest(convert_to((content-ARRAY['tables','functions','triggers','purgeGraph'])::text,'UTF8'),'sha256'),'hex'),
'tableCount',(SELECT count(*) FROM jsonb_object_keys(content->'tables')),
'functionCount',(SELECT count(*) FROM jsonb_object_keys(content->'functions')),
'triggerCount',(SELECT count(*) FROM jsonb_object_keys(content->'triggers')),
'purgeGraphDigest',encode(public.digest(convert_to((content->'purgeGraph')::text,'UTF8'),'sha256'),'hex'),
'purgeNodeCount',content#>'{purgeGraph,nodeCount}',
'purgeEdgeCount',content#>'{purgeGraph,edgeCount}',
'purgeNodeFingerprint',content#>'{purgeGraph,nodeFingerprint}',
'purgeEdgeFingerprint',content#>'{purgeGraph,edgeFingerprint}') FROM complete_catalog) INTO actual;
 IF actual IS DISTINCT FROM $expected${"authorityDigest":"1525715fe116df16749772105ed3f8ee7ab51ccfa0f9e23dc4b7fa6fbe8b7891","catalogDigest":"4e1adaf05bdc5cd8e80d311212a21a4faad709e10c1115a6bfaad44722979213","functionCount":233,"functionsDigest":"199acd1fa205096b3360b2bb3d7f05229e9fec4dad091d0cec2a9e6282bb47d2","purgeEdgeCount":320,"purgeEdgeFingerprint":"1cb01973881ffe170e65ca49f48a6289","purgeGraphDigest":"eee13fa1e944b17b75d63171e6c54fb8c5e375f2b7f56dd8b776241f554c18cb","purgeNodeCount":114,"purgeNodeFingerprint":"7e9c4f53e840517ede4723bab15cbbd5","tableCount":169,"tableDefaultACL":["{control_plane_runtime=arwd/control_plane_owner,kodex_backup_reader=r/control_plane_owner}"],"tablesDigest":"afc986b79a2271b7abdc5b6462e06e3cfaaae1fc72cf56c83d4bfc7a71defa80","triggerCount":187,"triggersDigest":"68027710f8f3dd534f7a1d7615e423dd069505d21c9528873e0538d4d830b345"}$expected$::jsonb THEN RAISE EXCEPTION 'Telegram notifications backup restore final catalog changed';END IF;
END $restore046$;
RESET ROLE;
