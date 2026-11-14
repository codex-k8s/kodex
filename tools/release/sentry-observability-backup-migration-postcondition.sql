-- Generated: generate-sentry-observability-proof.py; полный закрытый schema catalog.
BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout='15s';SET LOCAL lock_timeout='500ms';SET LOCAL search_path=pg_catalog;
DO $catalog_guard$ DECLARE actual jsonb;BEGIN IF (-- Закрытая identity sequence канонического bootstrap. Public foreign sequences
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
 AND (SELECT count(*) FROM acl WHERE grantee='kodex_backup_reader'::regrole) IN(0,2)) IS DISTINCT FROM true THEN RAISE EXCEPTION 'Sentry observability public sequence boundary changed';END IF;
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
'purgeEdgeFingerprint',content#>'{purgeGraph,edgeFingerprint}') FROM complete_catalog) INTO actual;IF actual IS DISTINCT FROM $expected0${"authorityDigest":"dbbc0b0fa31f980c0d6f63654f3830a268f596f4e39790b86fca5e58ff0e8583","catalogDigest":"7efe0778ea06685ae2bec2eb8cf6e8db476aa28c9154c89cce89ded4c08d4888","functionCount":228,"functionsDigest":"8b2272337516e0974a537530e892cd6c675a194512b76e8513f9d45e0d6b8b8b","purgeEdgeCount":310,"purgeEdgeFingerprint":"2f32d3b42715f0a7c905ddc9f6957e71","purgeGraphDigest":"3bb0a85fbf658cf7cdb6af06a76f2e0c6328560bcdc63c459d5185cb174aba84","purgeNodeCount":112,"purgeNodeFingerprint":"97039cc15f63901977000dabe1167d5d","tableCount":168,"tableDefaultACL":["{control_plane_runtime=arwd/control_plane_owner,kodex_backup_reader=r/control_plane_owner}"],"tablesDigest":"394202dda9b558f18777d1dabdf9e0129e5ce551de8b2e86d235a0782cdcb8ed","triggerCount":180,"triggersDigest":"225cb8882f62762c44b73ac5a877a2d062e6644d2903bfcf5be5312191e0265e"}$expected0$::jsonb AND actual IS DISTINCT FROM $expected1${"authorityDigest":"7c9c625617acd100ce4d5fe8c29722854d1083bf30f65aef0043a490f3dec03c","catalogDigest":"decd5c91c392daffc1476b84ef6db4b4d6b57a4203311bf3feb5abe839654814","functionCount":228,"functionsDigest":"8b2272337516e0974a537530e892cd6c675a194512b76e8513f9d45e0d6b8b8b","purgeEdgeCount":310,"purgeEdgeFingerprint":"2f32d3b42715f0a7c905ddc9f6957e71","purgeGraphDigest":"3bb0a85fbf658cf7cdb6af06a76f2e0c6328560bcdc63c459d5185cb174aba84","purgeNodeCount":112,"purgeNodeFingerprint":"97039cc15f63901977000dabe1167d5d","tableCount":168,"tableDefaultACL":["{control_plane_runtime=arwd/control_plane_owner,kodex_backup_reader=r/control_plane_owner}"],"tablesDigest":"394202dda9b558f18777d1dabdf9e0129e5ce551de8b2e86d235a0782cdcb8ed","triggerCount":180,"triggersDigest":"225cb8882f62762c44b73ac5a877a2d062e6644d2903bfcf5be5312191e0265e"}$expected1$::jsonb THEN RAISE EXCEPTION 'Sentry observability schema catalog changed';END IF;END $catalog_guard$;
SELECT jsonb_build_object('profile', 'SENTRY_OBSERVABILITY_BACKUP_V1', 'database',current_database(),'readOnly',current_setting('transaction_read_only'),'gooseVersion',(SELECT max(version_id) FROM public.goose_db_version WHERE is_applied),'schemaCatalog',(-- name: github_repository_scope_catalog :one
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
'purgeEdgeFingerprint',content#>'{purgeGraph,edgeFingerprint}') FROM complete_catalog), 'backupQualification',CASE WHEN EXISTS(SELECT 1 FROM pg_namespace n CROSS JOIN LATERAL aclexplode(n.nspacl) a WHERE n.oid='control_plane'::regnamespace AND a.grantee='kodex_backup_reader'::regrole AND a.privilege_type='USAGE') THEN 'CANONICAL_BACKUP' ELSE 'TABLE_ONLY_BACKUP' END, 'publicSequenceSafe',true)::text;
ROLLBACK;
