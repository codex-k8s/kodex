-- Generated: generate-personal-integrations-proof.py.
BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout='15s';SET LOCAL lock_timeout='500ms';SET LOCAL search_path=pg_catalog;
WITH qualified_catalog(value) AS MATERIALIZED (-- name: managed_kubernetes_catalog :one
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
'purgeEdgeFingerprint',content#>'{purgeGraph,edgeFingerprint}') FROM complete_catalog)
SELECT jsonb_build_object('profile','PERSONAL_INTEGRATIONS_BACKUP_V1',
 'database',current_database(),'readOnly',current_setting('transaction_read_only'),
 'gooseVersion',(SELECT max(version_id) FROM public.goose_db_version WHERE is_applied),
 'serverVersionNum',current_setting('server_version_num')::int,
 'historyMatches',((SELECT jsonb_agg(jsonb_build_array(version_id,is_applied) ORDER BY id)
 FROM public.goose_db_version) IS NOT DISTINCT FROM $history$[[0,true],[20260822000100,true],[20260828000100,true],[20260828000110,true],[20260828000200,true],[20260828099500,true],[20260828099600,true],[20260828099700,true],[20260828099800,true],[20260829000100,true],[20260829000200,true],[20260829000300,true],[20260829000400,true],[20260829000500,true],[20260829000600,true],[20260829000700,true],[20260829000800,true],[20260829000900,true],[20260829001000,true],[20260829001100,true],[20260829001200,true],[20260829001300,true],[20260830000100,true],[20260830000200,true],[20260830000300,true],[20260831000100,true],[20260831000200,true],[20260831000300,true],[20260831000400,true],[20260904000100,true],[20260904000200,true],[20260904000300,true],[20260904000400,true],[20260904000500,true],[20260904000600,true],[20260904000601,true],[20260904000602,true],[20260904000603,true],[20260904000604,true],[20260904000605,true],[20260904000606,true],[20260904000607,true],[20260904000608,true],[20260904000609,true],[20260904000610,true],[20260904000611,true],[20260904000612,true],[20260904000613,true],[20260904000614,true],[20260904000615,true],[20260904000616,true],[20260904000617,true],[20260904000618,true],[20260904000619,true],[20260904000620,true],[20260904000621,true],[20260904000622,true],[20260904000623,true],[20260904000624,true],[20260904000625,true],[20260904000626,true],[20260904000627,true],[20260904000630,true],[20260904000631,true],[20260904000632,true],[20260904000633,true],[20260904000634,true],[20260904000635,true],[20260904000636,true],[20260904000637,true],[20260904000638,true],[20260904000639,true],[20260904000640,true],[20260904000641,true],[20260904000643,true],[20260904000644,true],[20260904000646,true],[20260904000647,true],[20260905000642,true],[20260905000645,true],[20260906000648,true],[20260906000649,true],[20260906000650,true],[20260906000651,true],[20260907000400,true],[20260907000500,true],[20260907000600,true],[20260907000700,true],[20260908000300,true],[20260908000500,true],[20260913000100,true],[20260913000200,true],[20260913000300,true],[20260914000100,true],[20260916102000,true],[20260917000100,true],[20260917000200,true],[20260923000100,true],[20260923000200,true],[20260923000300,true],[20260923000400,true],[20260923000500,true],[20260923000600,true],[20260924000100,true],[20260924000200,true],[20260924000300,true],[20260924000400,true],[20260924000500,true],[20260924000600,true],[20260924000700,true],[20260924000800,true],[20260924000900,true],[20260924001000,true],[20260924001100,true],[20260924001200,true],[20260924001300,true],[20260924001400,true],[20260925000100,true],[20260925000200,true],[20260926000100,true],[20260926000200,true],[20260927000100,true],[20260927000200,true],[20260927000300,true],[20260927000400,true],[20260928000100,true],[20260928000200,true],[20260928000300,true],[20260928000400,true],[20260930000100,true],[20261001000100,true],[20261001000200,true],[20261001000300,true],[20261003000100,true],[20261003000300,true],[20261003000400,true],[20261003000500,true],[20261003000600,true],[20261003000700,true],[20261003000800,true],[20261003000900,true],[20261004000100,true],[20261004000200,true],[20261004000300,true],[20261004000400,true],[20261004000500,true],[20261004000600,true],[20261004000700,true],[20261004000800,true],[20261004001400,true],[20261004001500,true],[20261004001600,true],[20261004001700,true],[20261005000100,true],[20261005000200,true],[20261006000100,true],[20261006000300,true],[20261006000400,true],[20261007135000,true],[20261008000100,true],[20261010120000,true],[20261010130000,true],[20261010140000,true],[20261010150000,true],[20261014000100,true],[20261016000100,true],[20261019000100,true],[20261019000200,true],[20261019000300,true],[20261019000400,true],[20261020000100,true],[20261020000200,true],[20261025000100,true],[20261025000200,true],[20261026000100,true],[20261026000200,true],[20261026000300,true],[20261026000400,true],[20261026000500,true],[20261026000600,true],[20261026000700,true],[20261026000800,true],[20261026000900,true],[20261026001000,true],[20261026001100,true],[20261026001200,true],[20261026001300,true],[20261026001400,true],[20261026001500,true],[20261026001600,true],[20261026001700,true],[20261027000100,true],[20261028000100,true],[20261029000100,true],[20261029000200,true],[20261029000300,true],[20261030000100,true],[20261105000100,true],[20261106000100,true],[20261107000100,true],[20261108000100,true],[20261109000100,true],[20261110000100,true],[20261110000200,true],[20261111000100,true],[20261111000200,true],[20261112000100,true],[20261113000100,true],[20261114120000,true],[20261114130000,true],[20261114140000,true],[20261114150000,true],[20261114160000,true],[20261114170000,true]]$history$::jsonb),
 'backupQualification','CANONICAL_BACKUP','publicSequenceSafe',(-- Закрытая identity sequence канонического bootstrap. Public foreign sequences
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
 AND (SELECT count(*) FROM acl WHERE grantee='kodex_backup_reader'::regrole) IN(0,2)),
 'schemaCatalog',value,
 'schemaMatches',value=$expected${"authorityDigest":"16832868ccc8e1d5b9c57f34088a32f9ef39afb6ece2679856198119555b88d3","catalogDigest":"cec49c6edc276a09877e5b18ea3f4e196cb7fb069afbc8c53d4e1db685a1b4c7","functionCount":328,"functionsDigest":"2ef8dc26ea9a8b51fba0cfaa2b403a0f81e5042cd3d2075f9b219e37c550d23a","purgeEdgeCount":376,"purgeEdgeFingerprint":"512cddddde0e7b818ebcfe40f4719f3d","purgeGraphDigest":"3174dbb544ed14ce65c681c9486be01b626e81ccff448fce97e4d8730726395f","purgeNodeCount":139,"purgeNodeFingerprint":"fa3830df53c1264f778a60a4f269c5b8","tableCount":219,"tableDefaultACL":["{control_plane_runtime=arwd/control_plane_owner,kodex_backup_reader=r/control_plane_owner}"],"tablesDigest":"510795f1f04234b9c86fc84b6d1a855e32d1891b11f7f452b55f2ce787bf2dfd","triggerCount":282,"triggersDigest":"809e0f0703aa3ad0866d1d776756dff85a23421f9d6c70d543530c4c795c4927"}$expected$::jsonb)::text FROM qualified_catalog;
ROLLBACK;
