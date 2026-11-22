-- +goose Up
-- Общий metadata union не доставляет credential material и не изменяет старые receipts.
SET ROLE control_plane_owner;
SET LOCAL search_path=pg_catalog;
SET LOCAL lock_timeout='8s';
SELECT pg_advisory_xact_lock(hashtextextended('kodex.workspace.backup-acl-maintenance.v1',0));
-- BEGIN GENERATED MANAGED KUBERNETES PREDECESSOR
-- +goose StatementBegin
DO $closed_predecessor$
DECLARE actual jsonb;
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
 AND (SELECT count(*) FROM acl WHERE grantee='kodex_backup_reader'::regrole) IN(0,2)) IS DISTINCT FROM true THEN RAISE EXCEPTION 'managed Kubernetes public sequence boundary changed';END IF;
IF (-- ACL-only maintenance не меняет workspace rows, filesystem, writer custody
-- или quiescence. Historical BLOCKED/PENDING records сохраняются без трактовки
-- как released. Закрывается только реально активный owner work/grant graph.
SELECT NOT EXISTS(SELECT 1 FROM control_plane.runs WHERE state NOT IN('SUCCEEDED','FAILED','CANCELLED'))
 AND NOT EXISTS(SELECT 1 FROM control_plane.run_nodes WHERE state IN('QUEUED','RUNNING','WAITING'))
 AND NOT EXISTS(SELECT 1 FROM control_plane.runtime_leases WHERE state='CLAIMED')
 AND NOT EXISTS(SELECT 1 FROM control_plane.session_archive_tasks WHERE state IN('READY','CLAIMED'))
 AND NOT EXISTS(SELECT 1 FROM control_plane.session_workspace_volume_tasks WHERE state IN('READY','CLAIMED'))
 -- Даже expired ACTIVE grant не считается CLOSED по времени.
 AND NOT EXISTS(SELECT 1 FROM control_plane.session_workspace_volume_attempts WHERE grant_state='ACTIVE')) IS DISTINCT FROM true THEN RAISE EXCEPTION 'managed Kubernetes workspace owner is not idle';END IF;
SELECT (-- name: managed_kubernetes_catalog :one
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
IF actual IS DISTINCT FROM $predecessor0${"authorityDigest":"48e658fe0cec5baa0c09f63d38f3b6d1057104cc1726a0f9c8b74be6497957a7","catalogDigest":"ad3495f152515b4d7d4dbdad522143c28bc25aa2d72ca83fbcaa820929611bfb","functionCount":233,"functionsDigest":"199acd1fa205096b3360b2bb3d7f05229e9fec4dad091d0cec2a9e6282bb47d2","purgeEdgeCount":320,"purgeEdgeFingerprint":"1cb01973881ffe170e65ca49f48a6289","purgeGraphDigest":"eee13fa1e944b17b75d63171e6c54fb8c5e375f2b7f56dd8b776241f554c18cb","purgeNodeCount":114,"purgeNodeFingerprint":"7e9c4f53e840517ede4723bab15cbbd5","tableCount":169,"tableDefaultACL":["{control_plane_runtime=arwd/control_plane_owner}"],"tablesDigest":"f5f2e9b961fd2cd40188cd6928b021ddfaf40972b47c72812c601c9607f8c3eb","triggerCount":187,"triggersDigest":"68027710f8f3dd534f7a1d7615e423dd069505d21c9528873e0538d4d830b345"}$predecessor0$::jsonb AND actual IS DISTINCT FROM $predecessor1${"authorityDigest":"1525715fe116df16749772105ed3f8ee7ab51ccfa0f9e23dc4b7fa6fbe8b7891","catalogDigest":"4e1adaf05bdc5cd8e80d311212a21a4faad709e10c1115a6bfaad44722979213","functionCount":233,"functionsDigest":"199acd1fa205096b3360b2bb3d7f05229e9fec4dad091d0cec2a9e6282bb47d2","purgeEdgeCount":320,"purgeEdgeFingerprint":"1cb01973881ffe170e65ca49f48a6289","purgeGraphDigest":"eee13fa1e944b17b75d63171e6c54fb8c5e375f2b7f56dd8b776241f554c18cb","purgeNodeCount":114,"purgeNodeFingerprint":"7e9c4f53e840517ede4723bab15cbbd5","tableCount":169,"tableDefaultACL":["{control_plane_runtime=arwd/control_plane_owner,kodex_backup_reader=r/control_plane_owner}"],"tablesDigest":"afc986b79a2271b7abdc5b6462e06e3cfaaae1fc72cf56c83d4bfc7a71defa80","triggerCount":187,"triggersDigest":"68027710f8f3dd534f7a1d7615e423dd069505d21c9528873e0538d4d830b345"}$predecessor1$::jsonb AND actual IS DISTINCT FROM $predecessor2${"authorityDigest":"ddd91304bc9957a61c373ef08b3f6ba9b09987256cb54b4ad130c23b1120f3b8","catalogDigest":"2732ad259f62c377994a155a6cfe57dc978a2194df58e012fb9a395b92d0bcbb","functionCount":233,"functionsDigest":"199acd1fa205096b3360b2bb3d7f05229e9fec4dad091d0cec2a9e6282bb47d2","purgeEdgeCount":320,"purgeEdgeFingerprint":"1cb01973881ffe170e65ca49f48a6289","purgeGraphDigest":"eee13fa1e944b17b75d63171e6c54fb8c5e375f2b7f56dd8b776241f554c18cb","purgeNodeCount":114,"purgeNodeFingerprint":"7e9c4f53e840517ede4723bab15cbbd5","tableCount":169,"tableDefaultACL":["{control_plane_runtime=arwd/control_plane_owner,kodex_backup_reader=r/control_plane_owner}"],"tablesDigest":"afc986b79a2271b7abdc5b6462e06e3cfaaae1fc72cf56c83d4bfc7a71defa80","triggerCount":187,"triggersDigest":"68027710f8f3dd534f7a1d7615e423dd069505d21c9528873e0538d4d830b345"}$predecessor2$::jsonb THEN RAISE EXCEPTION 'managed Kubernetes exact predecessor catalog changed';END IF;
END $closed_predecessor$;
-- +goose StatementEnd
-- END GENERATED MANAGED KUBERNETES PREDECESSOR

-- Закрытое расширение трёх owner CHECK; другие adapters/resource kinds не изменяются.
-- +goose StatementBegin
DO $kubernetes_kind_successors$
DECLARE item record; source text; relation_name regclass; constraint_name text; marker text; replacement text;
BEGIN
 FOR relation_name,constraint_name,marker,replacement IN VALUES
  ('control_plane.integration_definitions'::regclass,'integration_definitions_adapter_check',
   '''TELEGRAM_INTERACTION''::text','''TELEGRAM_INTERACTION''::text, ''KUBERNETES''::text'),
  ('control_plane.integration_grants'::regclass,'integration_grants_resource_kind_check',
   '''SSH_TARGET''::text','''SSH_TARGET''::text, ''KUBERNETES_TARGET''::text'),
  ('control_plane.integration_invocations'::regclass,'integration_invocations_resource_kind_check',
   '''SSH_TARGET''::text','''SSH_TARGET''::text, ''KUBERNETES_TARGET''::text')
 LOOP
  SELECT conname,pg_get_constraintdef(oid) AS definition INTO STRICT item
  FROM pg_constraint WHERE conrelid=relation_name AND conname=constraint_name AND contype='c';
  source:=item.definition;
  IF (length(source)-length(replace(source,marker,'')))/length(marker)<>1
   OR position('KUBERNETES' IN source)>0 THEN RAISE EXCEPTION 'managed Kubernetes kind predecessor changed';END IF;
  EXECUTE format('ALTER TABLE %s DROP CONSTRAINT %I, ADD CONSTRAINT %I %s',
   relation_name,item.conname,item.conname,replace(source,marker,replacement));
 END LOOP;
END $kubernetes_kind_successors$;
-- +goose StatementEnd

-- Operator metadata не содержит kubeconfig/keys. История immutable;
-- current pointer не подменяет identity либо effect receipts.
CREATE TABLE control_plane.kubernetes_operator_registry_revisions (
 revision bigint PRIMARY KEY CHECK (revision BETWEEN 1 AND 9007199254740991),
 installation_ref text NOT NULL CHECK (installation_ref ~ '^[A-Za-z0-9_-]{8,96}$'),
 identity_namespace text NOT NULL CHECK (identity_namespace ~ '^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$'),
 payload text NOT NULL CHECK (octet_length(payload) BETWEEN 1 AND 1048576),
 content_sha256 text NOT NULL UNIQUE CHECK (content_sha256 ~ '^[a-f0-9]{64}$'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK (content_sha256=encode(public.digest(convert_to(payload,'UTF8'),'sha256'),'hex')),
 CHECK ((payload::jsonb->>'schema') IS NOT DISTINCT FROM 'managed-kubernetes-operator/v1'
  AND (payload::jsonb->>'managedBy') IS NOT DISTINCT FROM 'git'
  AND (payload::jsonb->>'source') IS NOT DISTINCT FROM 'repo:kubernetes-operator'
  AND (payload::jsonb->>'revision')::bigint IS NOT DISTINCT FROM revision
  AND (payload::jsonb->>'installationRef') IS NOT DISTINCT FROM installation_ref
  AND (payload::jsonb->>'identityNamespace') IS NOT DISTINCT FROM identity_namespace)
);
CREATE TABLE control_plane.kubernetes_operator_current (
 singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
 revision bigint NOT NULL REFERENCES control_plane.kubernetes_operator_registry_revisions(revision),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE control_plane.kubernetes_target_revisions (
 ref text NOT NULL CHECK (ref ~ '^[A-Za-z0-9_-]{8,96}$'),
 revision bigint NOT NULL CHECK (revision BETWEEN 1 AND 9007199254740991),
 registry_revision bigint NOT NULL REFERENCES control_plane.kubernetes_operator_registry_revisions(revision),
 cluster_uid uuid NOT NULL CHECK (cluster_uid<>'00000000-0000-0000-0000-000000000000'::uuid),
 payload text NOT NULL CHECK (octet_length(payload) BETWEEN 1 AND 163840),
 content_sha256 text NOT NULL CHECK (content_sha256 ~ '^[a-f0-9]{64}$'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY (ref,revision),
 UNIQUE (ref,revision,cluster_uid),
 CHECK (content_sha256=encode(public.digest(convert_to(payload,'UTF8'),'sha256'),'hex')),
 CHECK ((payload::jsonb#>>'{target,ref}') IS NOT DISTINCT FROM ref
  AND (payload::jsonb#>>'{target,revision}')::bigint IS NOT DISTINCT FROM revision
  AND (payload::jsonb#>>'{target,clusterUID}') IS NOT DISTINCT FROM cluster_uid::text)
);
CREATE TABLE control_plane.kubernetes_serving_key_owners (
 first_target_ref text NOT NULL,
 first_target_revision bigint NOT NULL,
 serving_key_sha256 text NOT NULL CHECK (serving_key_sha256 ~ '^[a-f0-9]{64}$'),
 cluster_uid uuid NOT NULL CHECK (cluster_uid<>'00000000-0000-0000-0000-000000000000'::uuid),
 node_uid uuid NOT NULL CHECK (node_uid<>'00000000-0000-0000-0000-000000000000'::uuid),
 registry_revision bigint NOT NULL REFERENCES control_plane.kubernetes_operator_registry_revisions(revision),
 PRIMARY KEY (serving_key_sha256),
 UNIQUE (serving_key_sha256,cluster_uid,node_uid),
 FOREIGN KEY (first_target_ref,first_target_revision,cluster_uid) REFERENCES control_plane.kubernetes_target_revisions(ref,revision,cluster_uid)
);
CREATE TABLE control_plane.kubernetes_node_transport_pins (
 ref text PRIMARY KEY CHECK (ref ~ '^[A-Za-z0-9_-]{8,96}$'),
 generation bigint NOT NULL CHECK (generation BETWEEN 1 AND 9007199254740991),
 target_ref text NOT NULL,
 target_revision bigint NOT NULL,
 cluster_uid uuid NOT NULL CHECK (cluster_uid<>'00000000-0000-0000-0000-000000000000'::uuid),
 node_uid uuid NOT NULL CHECK (node_uid<>'00000000-0000-0000-0000-000000000000'::uuid),
 protocol text NOT NULL CHECK (protocol IN ('WEBSOCKET_V5','SPDY_V4','PORT_FORWARD_WEBSOCKET_V4','PORT_FORWARD_SPDY_V1')),
 serving_key_sha256 text NOT NULL CHECK (serving_key_sha256 ~ '^[a-f0-9]{64}$'),
 registry_revision bigint NOT NULL REFERENCES control_plane.kubernetes_operator_registry_revisions(revision),
 payload text NOT NULL CHECK (octet_length(payload) BETWEEN 1 AND 98304),
 content_sha256 text NOT NULL CHECK (content_sha256 ~ '^[a-f0-9]{64}$'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE (cluster_uid,node_uid,protocol,generation),
 FOREIGN KEY (target_ref,target_revision,cluster_uid) REFERENCES control_plane.kubernetes_target_revisions(ref,revision,cluster_uid),
 FOREIGN KEY (serving_key_sha256,cluster_uid,node_uid)
  REFERENCES control_plane.kubernetes_serving_key_owners(serving_key_sha256,cluster_uid,node_uid),
 CHECK (content_sha256=encode(public.digest(convert_to(payload,'UTF8'),'sha256'),'hex')),
 CHECK ((payload::jsonb->>'ref') IS NOT DISTINCT FROM ref
  AND (payload::jsonb->>'generation')::bigint IS NOT DISTINCT FROM generation
  AND (payload::jsonb->>'targetRef') IS NOT DISTINCT FROM target_ref
  AND (payload::jsonb->>'targetRevision')::bigint IS NOT DISTINCT FROM target_revision
  AND (payload::jsonb->>'nodeUID') IS NOT DISTINCT FROM node_uid::text
  AND (payload::jsonb->>'protocol') IS NOT DISTINCT FROM protocol
  AND (payload::jsonb->>'serverPublicKeySHA256') IS NOT DISTINCT FROM serving_key_sha256)
);
CREATE TABLE control_plane.kubernetes_retired_node_pins (
 ref text PRIMARY KEY REFERENCES control_plane.kubernetes_node_transport_pins(ref),
 registry_revision bigint NOT NULL REFERENCES control_plane.kubernetes_operator_registry_revisions(revision),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE control_plane.kubernetes_retired_serving_keys (
 serving_key_sha256 text PRIMARY KEY REFERENCES control_plane.kubernetes_serving_key_owners(serving_key_sha256),
 registry_revision bigint NOT NULL REFERENCES control_plane.kubernetes_operator_registry_revisions(revision),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

-- +goose StatementBegin
DO $immutable_operator$
DECLARE relation_name text;
BEGIN
 FOREACH relation_name IN ARRAY ARRAY['kubernetes_operator_registry_revisions','kubernetes_target_revisions',
  'kubernetes_serving_key_owners','kubernetes_node_transport_pins','kubernetes_retired_node_pins','kubernetes_retired_serving_keys']
 LOOP
  EXECUTE format('CREATE TRIGGER %I BEFORE UPDATE OR DELETE ON control_plane.%I FOR EACH ROW EXECUTE FUNCTION control_plane.reject_integration_immutable_update()',relation_name||'_immutable',relation_name);
  EXECUTE format('REVOKE ALL ON control_plane.%I FROM control_plane_runtime',relation_name);
  EXECUTE format('GRANT SELECT,INSERT ON control_plane.%I TO control_plane_runtime',relation_name);
 END LOOP;
END $immutable_operator$;
-- +goose StatementEnd
REVOKE ALL ON control_plane.kubernetes_operator_current FROM control_plane_runtime;
GRANT SELECT,INSERT,UPDATE ON control_plane.kubernetes_operator_current TO control_plane_runtime;

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_kubernetes_operator_current() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,control_plane AS $operator_current$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'Kubernetes operator history cannot be deleted';END IF;
 IF TG_OP='UPDATE' AND (NEW.singleton IS DISTINCT FROM OLD.singleton OR NEW.revision<=OLD.revision OR NOT EXISTS(
  SELECT 1 FROM control_plane.kubernetes_operator_registry_revisions previous_revision
  JOIN control_plane.kubernetes_operator_registry_revisions next_revision ON next_revision.revision=NEW.revision
  WHERE previous_revision.revision=OLD.revision AND previous_revision.installation_ref=next_revision.installation_ref
   AND previous_revision.identity_namespace=next_revision.identity_namespace
 )) THEN RAISE EXCEPTION 'Kubernetes operator source must advance within its installation';END IF;
 NEW.updated_at:=clock_timestamp();
 RETURN NEW;
END $operator_current$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_kubernetes_operator_current() FROM PUBLIC;
CREATE TRIGGER kubernetes_operator_current_forward
 BEFORE UPDATE OR DELETE ON control_plane.kubernetes_operator_current
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_kubernetes_operator_current();

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_kubernetes_target_revision() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,control_plane AS $target_revision$
BEGIN
 IF EXISTS(SELECT 1 FROM control_plane.kubernetes_target_revisions previous_revision
  WHERE previous_revision.ref=NEW.ref AND previous_revision.cluster_uid<>NEW.cluster_uid)
 OR EXISTS(SELECT 1 FROM control_plane.kubernetes_target_revisions previous_revision
  CROSS JOIN LATERAL jsonb_array_elements(previous_revision.payload::jsonb->'protectedNamespaces') protected
  WHERE previous_revision.cluster_uid=NEW.cluster_uid AND NOT EXISTS(
   SELECT 1 FROM jsonb_array_elements(NEW.payload::jsonb->'protectedNamespaces') retained
   WHERE retained->>'name'=protected->>'name')) THEN
  RAISE EXCEPTION 'Kubernetes target identity or protection cannot be reassigned';
 END IF;
 RETURN NEW;
END $target_revision$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_kubernetes_target_revision() FROM PUBLIC;
CREATE TRIGGER kubernetes_target_revision_owner
 BEFORE INSERT ON control_plane.kubernetes_target_revisions
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_kubernetes_target_revision();

-- Существующие schema046 bindings заведомо TOKEN. Это явное назначение нового
-- stored discriminator в миграции, а не inference из будущего runtime payload.
-- Сохранённые idempotency receipts и immutable token revisions не переписываются.
ALTER TABLE control_plane.integration_personal_credentials
 ADD COLUMN credential_kind text NOT NULL DEFAULT 'TOKEN'
  CHECK (credential_kind IN ('TOKEN','BROKER_IDENTITY')),
 ADD COLUMN broker_purpose text CHECK (broker_purpose IN ('KUBERNETES_TARGET','SSH_TARGET')),
 ADD COLUMN broker_identity_revision_id uuid,
 ADD COLUMN verified_broker_identity_revision_id uuid;

-- Общая immutable revision хранит только metadata. Приватные pins принадлежат
-- отдельным purpose таблицам, поэтому SSH не получает Kubernetes Secret shape.
CREATE TABLE control_plane.integration_broker_identity_revisions (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 ref text NOT NULL UNIQUE CHECK (ref ~ '^[A-Za-z0-9_-]{8,96}$'),
 organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
 connection_id uuid NOT NULL,
 binding_id uuid,
 subject_id uuid NOT NULL,
 credential_policy text NOT NULL CHECK (credential_policy IN ('INITIATOR_REQUIRED','PROJECT_SHARED')),
 purpose text NOT NULL CHECK (purpose IN ('KUBERNETES_TARGET','SSH_TARGET')),
 target_ref text NOT NULL CHECK (target_ref ~ '^[A-Za-z0-9_-]{8,96}$'),
 revision bigint NOT NULL CHECK (revision BETWEEN 1 AND 9007199254740991),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 FOREIGN KEY (organization_id,connection_id)
  REFERENCES control_plane.integration_connections(organization_id,id),
 FOREIGN KEY (organization_id,subject_id)
  REFERENCES control_plane.subjects(organization_id,id),
 FOREIGN KEY (organization_id,binding_id,connection_id,subject_id)
  REFERENCES control_plane.integration_personal_credentials(organization_id,id,connection_id,subject_id),
 CHECK ((credential_policy='INITIATOR_REQUIRED' AND binding_id IS NOT NULL)
  OR (credential_policy='PROJECT_SHARED' AND binding_id IS NULL)),
 UNIQUE (organization_id,id),
 UNIQUE (organization_id,id,purpose),
 UNIQUE (organization_id,binding_id,purpose,id),
 UNIQUE (organization_id,connection_id,id),
 UNIQUE (organization_id,connection_id,subject_id,purpose,revision)
);
CREATE TRIGGER integration_broker_identity_revisions_immutable
 BEFORE UPDATE OR DELETE ON control_plane.integration_broker_identity_revisions
 FOR EACH ROW EXECUTE FUNCTION control_plane.reject_integration_immutable_update();

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_integration_broker_identity_revision() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,control_plane AS $broker_identity$
BEGIN
 IF NOT EXISTS (
  SELECT 1 FROM control_plane.integration_connections c
  JOIN control_plane.subjects s ON s.id=NEW.subject_id AND s.organization_id=c.organization_id AND s.active
  WHERE c.id=NEW.connection_id AND c.organization_id=NEW.organization_id AND c.lifecycle_state='ACTIVE'
   AND c.credential_policy=NEW.credential_policy
   AND ((NEW.credential_policy='PROJECT_SHARED' AND s.kind='SERVICE' AND NEW.binding_id IS NULL)
    OR (NEW.credential_policy='INITIATOR_REQUIRED' AND s.kind='USER' AND EXISTS (
     SELECT 1 FROM control_plane.integration_personal_credentials b
     WHERE b.id=NEW.binding_id AND b.organization_id=NEW.organization_id AND b.connection_id=c.id
      AND b.subject_id=s.id AND b.credential_kind='BROKER_IDENTITY' AND b.broker_purpose=NEW.purpose)))
 ) THEN RAISE EXCEPTION 'broker identity owner boundary changed';END IF;
 RETURN NEW;
END $broker_identity$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_integration_broker_identity_revision() FROM PUBLIC;
CREATE TRIGGER integration_broker_identity_owner
 BEFORE INSERT ON control_plane.integration_broker_identity_revisions
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_integration_broker_identity_revision();

-- Setup хранит только owner metadata, immutable content hash и digest permit.
-- Raw permit и kubeconfig отсутствуют в owner DB, audit и public receipts.
CREATE TABLE control_plane.kubernetes_identity_setups (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 ref text NOT NULL UNIQUE CHECK (ref ~ '^ksu_[A-Za-z0-9_-]{8,92}$'),
 organization_id uuid NOT NULL,
 connection_id uuid NOT NULL,
 actor_id uuid NOT NULL,
 identity_revision_id uuid NOT NULL UNIQUE,
 registry_revision bigint NOT NULL REFERENCES control_plane.kubernetes_operator_registry_revisions(revision),
 target_ref text NOT NULL,
 target_revision bigint NOT NULL,
 expected_connection_version bigint NOT NULL CHECK (expected_connection_version>0),
 expected_binding_version bigint NOT NULL CHECK (expected_binding_version>=0),
 expected_binding_generation bigint NOT NULL CHECK (expected_binding_generation>=0),
 definition_digest text NOT NULL CHECK (definition_digest ~ '^[a-f0-9]{64}$'),
 configuration_digest text NOT NULL CHECK (configuration_digest ~ '^[a-f0-9]{64}$'),
 begin_idempotency_key text NOT NULL CHECK (octet_length(begin_idempotency_key) BETWEEN 8 AND 128),
 intent_sha256 text NOT NULL CHECK (intent_sha256 ~ '^[a-f0-9]{64}$'),
 permit_sha256 text NOT NULL UNIQUE CHECK (permit_sha256 ~ '^[a-f0-9]{64}$'),
 snapshot_sha256 text NOT NULL CHECK (snapshot_sha256 ~ '^[a-f0-9]{64}$'),
 content_sha256 text NOT NULL CHECK (content_sha256 ~ '^[a-f0-9]{64}$'),
 content_bytes integer NOT NULL CHECK (content_bytes BETWEEN 1 AND 1048576),
 state text NOT NULL DEFAULT 'PREPARED' CHECK (state IN('PREPARED','ATTEMPTING','UNKNOWN','COMPLETED','CANCELLED','EXPIRED','ACK_REJECTED')),
 version bigint NOT NULL DEFAULT 1 CHECK (version>0),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 expires_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 FOREIGN KEY (organization_id,connection_id) REFERENCES control_plane.integration_connections(organization_id,id),
 FOREIGN KEY (organization_id,actor_id) REFERENCES control_plane.subjects(organization_id,id),
 FOREIGN KEY (organization_id,connection_id,identity_revision_id)
  REFERENCES control_plane.integration_broker_identity_revisions(organization_id,connection_id,id),
 FOREIGN KEY (target_ref,target_revision) REFERENCES control_plane.kubernetes_target_revisions(ref,revision),
 CHECK (expires_at>created_at AND expires_at<=created_at+interval '90 seconds'),
 UNIQUE (organization_id,actor_id,begin_idempotency_key),
 UNIQUE (organization_id,id)
);

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_kubernetes_identity_setup() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,control_plane AS $setup_owner$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'Kubernetes identity setup history cannot be deleted';END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.state<>'PREPARED' OR NEW.version<>1 OR NOT EXISTS (
   SELECT 1 FROM control_plane.integration_broker_identity_revisions identity
   JOIN control_plane.subjects actor ON actor.organization_id=identity.organization_id
    AND actor.id=NEW.actor_id AND actor.kind='USER' AND actor.active
   JOIN control_plane.integration_connections connection ON connection.id=identity.connection_id
    AND connection.organization_id=identity.organization_id AND connection.lifecycle_state='ACTIVE'
    AND connection.version=NEW.expected_connection_version
    AND connection.credential_policy=identity.credential_policy
   JOIN control_plane.kubernetes_operator_current current ON current.revision=NEW.registry_revision
   JOIN control_plane.kubernetes_target_revisions target ON target.ref=NEW.target_ref
    AND target.revision=NEW.target_revision AND target.registry_revision<=current.revision
   JOIN control_plane.kubernetes_operator_registry_revisions registry ON registry.revision=current.revision
   WHERE identity.id=NEW.identity_revision_id AND identity.organization_id=NEW.organization_id
    AND identity.connection_id=NEW.connection_id AND identity.purpose='KUBERNETES_TARGET'
    AND identity.target_ref=NEW.target_ref
    AND EXISTS(SELECT 1 FROM jsonb_array_elements(registry.payload::jsonb->'targets') entry
     WHERE entry->'target'->>'ref'=target.ref AND (entry->'target'->>'revision')::bigint=target.revision)
    AND ((identity.credential_policy='INITIATOR_REQUIRED' AND identity.subject_id=NEW.actor_id
     AND EXISTS(SELECT 1 FROM control_plane.integration_personal_credentials binding
      WHERE binding.id=identity.binding_id AND binding.organization_id=identity.organization_id
       AND binding.version=NEW.expected_binding_version AND binding.generation=NEW.expected_binding_generation))
     OR identity.credential_policy='PROJECT_SHARED' AND identity.binding_id IS NULL
      AND NEW.expected_binding_version=0 AND NEW.expected_binding_generation=0)
  ) THEN RAISE EXCEPTION 'Kubernetes identity setup owner boundary changed';END IF;
 ELSE
  IF (to_jsonb(NEW)-ARRAY['state','version','updated_at']) IS DISTINCT FROM
    (to_jsonb(OLD)-ARRAY['state','version','updated_at'])
   OR OLD.version=9223372036854775807 OR NEW.version<>OLD.version+1
   OR OLD.state IN('COMPLETED','CANCELLED','EXPIRED','ACK_REJECTED')
   OR NOT ((OLD.state='PREPARED' AND NEW.state IN('ATTEMPTING','CANCELLED','EXPIRED','ACK_REJECTED'))
    OR (OLD.state IN('ATTEMPTING','UNKNOWN') AND NEW.state IN('UNKNOWN','COMPLETED','CANCELLED','EXPIRED','ACK_REJECTED'))) THEN
   RAISE EXCEPTION 'Kubernetes identity setup transition is invalid';
  END IF;
  NEW.updated_at:=clock_timestamp();
 END IF;
 RETURN NEW;
END $setup_owner$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_kubernetes_identity_setup() FROM PUBLIC;
CREATE TRIGGER kubernetes_identity_setup_owner BEFORE INSERT OR UPDATE OR DELETE
 ON control_plane.kubernetes_identity_setups FOR EACH ROW
 EXECUTE FUNCTION control_plane.guard_kubernetes_identity_setup();

CREATE TABLE control_plane.kubernetes_identity_upload_attempts (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 ref text NOT NULL UNIQUE CHECK (ref ~ '^ksa_[A-Za-z0-9_-]{8,92}$'),
 organization_id uuid NOT NULL,
 setup_id uuid NOT NULL,
 generation bigint NOT NULL CHECK (generation BETWEEN 1 AND 8),
 mode text NOT NULL CHECK (mode IN('WRITE','OBSERVE')),
 workload_instance text NOT NULL CHECK (octet_length(workload_instance) BETWEEN 1 AND 256),
 fence_sha256 text NOT NULL UNIQUE CHECK (fence_sha256 ~ '^[a-f0-9]{64}$'),
 state text NOT NULL DEFAULT 'ACTIVE' CHECK (state IN('ACTIVE','COMPLETED','UNKNOWN','CANCELLED','EXPIRED','ACK_REJECTED')),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 expires_at timestamptz NOT NULL,
 closed_at timestamptz,
 FOREIGN KEY (organization_id,setup_id) REFERENCES control_plane.kubernetes_identity_setups(organization_id,id),
 CHECK ((state='ACTIVE' AND closed_at IS NULL) OR (state<>'ACTIVE' AND closed_at IS NOT NULL)),
 CHECK (expires_at>created_at AND expires_at<=created_at+interval '30 seconds'),
 UNIQUE (setup_id,generation)
);
CREATE UNIQUE INDEX kubernetes_identity_one_write ON control_plane.kubernetes_identity_upload_attempts(setup_id) WHERE mode='WRITE';
CREATE UNIQUE INDEX kubernetes_identity_one_active_attempt ON control_plane.kubernetes_identity_upload_attempts(setup_id) WHERE state='ACTIVE';

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_kubernetes_identity_upload_attempt() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,control_plane AS $setup_attempt$
DECLARE setup control_plane.kubernetes_identity_setups%ROWTYPE; previous_generation bigint;
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'Kubernetes identity attempt history cannot be deleted';END IF;
 IF TG_OP='INSERT' THEN
  SELECT * INTO setup FROM control_plane.kubernetes_identity_setups WHERE id=NEW.setup_id FOR UPDATE;
  SELECT coalesce(max(generation),0) INTO previous_generation
   FROM control_plane.kubernetes_identity_upload_attempts WHERE setup_id=NEW.setup_id;
  IF setup.organization_id IS DISTINCT FROM NEW.organization_id OR setup.state NOT IN('PREPARED','ATTEMPTING','UNKNOWN')
   OR setup.expires_at<=clock_timestamp() OR NEW.expires_at>setup.expires_at
   OR NEW.generation<>previous_generation+1 OR NEW.state<>'ACTIVE'
   OR (NEW.mode='WRITE' AND (setup.state<>'PREPARED' OR NEW.generation<>1))
   OR (NEW.mode='OBSERVE' AND NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_identity_upload_attempts
    WHERE setup_id=NEW.setup_id AND mode='WRITE')) THEN
   RAISE EXCEPTION 'Kubernetes identity attempt admission changed';
  END IF;
 ELSE
  IF (to_jsonb(NEW)-ARRAY['state','closed_at']) IS DISTINCT FROM (to_jsonb(OLD)-ARRAY['state','closed_at'])
   OR OLD.state<>'ACTIVE' OR NEW.state='ACTIVE' OR NEW.closed_at IS NULL THEN
   RAISE EXCEPTION 'Kubernetes identity attempt cannot be replayed';
  END IF;
 END IF;
 RETURN NEW;
END $setup_attempt$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_kubernetes_identity_upload_attempt() FROM PUBLIC;
CREATE TRIGGER kubernetes_identity_upload_attempt_owner BEFORE INSERT OR UPDATE OR DELETE
 ON control_plane.kubernetes_identity_upload_attempts FOR EACH ROW
 EXECUTE FUNCTION control_plane.guard_kubernetes_identity_upload_attempt();
REVOKE ALL ON control_plane.kubernetes_identity_setups,control_plane.kubernetes_identity_upload_attempts FROM control_plane_runtime;
GRANT SELECT,INSERT,UPDATE ON control_plane.kubernetes_identity_setups,control_plane.kubernetes_identity_upload_attempts TO control_plane_runtime;

-- Полный invalid successful ACK — отдельная terminal квитанция, не UNKNOWN.
-- Raw response body не сохраняется; UID witness не является usable locator.
CREATE TABLE control_plane.kubernetes_identity_acknowledgement_rejections (
 attempt_id uuid PRIMARY KEY REFERENCES control_plane.kubernetes_identity_upload_attempts(id),
 response_sha256 text NOT NULL CHECK (response_sha256 ~ '^[a-f0-9]{64}$'),
 truncated boolean NOT NULL,
 observed_uid uuid CHECK (observed_uid<>'00000000-0000-0000-0000-000000000000'::uuid),
 conflicts_with_known boolean NOT NULL DEFAULT false,
 observed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK (NOT truncated OR observed_uid IS NULL)
);
CREATE TRIGGER kubernetes_identity_acknowledgement_rejections_immutable BEFORE UPDATE OR DELETE
 ON control_plane.kubernetes_identity_acknowledgement_rejections FOR EACH ROW
 EXECUTE FUNCTION control_plane.reject_integration_immutable_update();
REVOKE ALL ON control_plane.kubernetes_identity_acknowledgement_rejections FROM control_plane_runtime;
GRANT SELECT,INSERT ON control_plane.kubernetes_identity_acknowledgement_rejections TO control_plane_runtime;

ALTER TABLE control_plane.integration_personal_credentials
 DROP CONSTRAINT integration_personal_credentials_check,
 DROP CONSTRAINT integration_personal_credentials_check1,
 ADD CONSTRAINT integration_personal_credential_kind_oneof CHECK (
  (credential_kind='TOKEN' AND broker_purpose IS NULL AND broker_identity_revision_id IS NULL AND verified_broker_identity_revision_id IS NULL)
  OR (credential_kind='BROKER_IDENTITY' AND broker_purpose IS NOT NULL AND credential_revision_id IS NULL AND verified_credential_revision_id IS NULL)),
 ADD CONSTRAINT integration_personal_source_state CHECK (
  (state='NOT_CONFIGURED' AND credential_revision_id IS NULL AND broker_identity_revision_id IS NULL)
  OR (state IN ('CHECK_REQUIRED','CHECKING','READY','INVALID') AND num_nonnulls(credential_revision_id,broker_identity_revision_id)=1)
  OR state='REVOKED'),
 ADD CONSTRAINT integration_personal_verified_source_state CHECK (
  (state='READY' AND verified_definition_digest IS NOT NULL AND verified_configuration_digest IS NOT NULL AND
   ((credential_kind='TOKEN' AND verified_credential_revision_id=credential_revision_id AND verified_credential_revision_id IS NOT NULL)
    OR (credential_kind='BROKER_IDENTITY' AND verified_broker_identity_revision_id=broker_identity_revision_id AND verified_broker_identity_revision_id IS NOT NULL)))
  OR (state<>'READY' AND verified_credential_revision_id IS NULL AND verified_broker_identity_revision_id IS NULL
   AND verified_definition_digest IS NULL AND verified_configuration_digest IS NULL AND verified_capabilities='{}'
   AND denied_capabilities='{}' AND observed_permission_profiles='{}' AND expires_at IS NULL)),
 ADD CONSTRAINT integration_personal_current_broker_identity_owner
  FOREIGN KEY (organization_id,id,broker_purpose,broker_identity_revision_id)
  REFERENCES control_plane.integration_broker_identity_revisions(organization_id,binding_id,purpose,id),
 ADD CONSTRAINT integration_personal_verified_broker_identity_owner
  FOREIGN KEY (organization_id,id,broker_purpose,verified_broker_identity_revision_id)
  REFERENCES control_plane.integration_broker_identity_revisions(organization_id,binding_id,purpose,id);

ALTER TABLE control_plane.integration_connections
 ADD COLUMN broker_identity_revision_id uuid,
 ADD CONSTRAINT integration_connection_credential_source_oneof CHECK (
  num_nonnulls(credential_revision_id,broker_identity_revision_id)<=1),
 ADD CONSTRAINT integration_connection_broker_identity_owner
  FOREIGN KEY (organization_id,id,broker_identity_revision_id)
  REFERENCES control_plane.integration_broker_identity_revisions(organization_id,connection_id,id);

-- Только purpose KUBERNETES_TARGET получает эту private таблицу; SSH048 использует
-- свою таблицу с отдельным purpose FK. Generic metadata projections её не читают.
CREATE TABLE control_plane.kubernetes_identity_material_revisions (
 organization_id uuid NOT NULL,
 identity_revision_id uuid PRIMARY KEY,
 purpose text NOT NULL DEFAULT 'KUBERNETES_TARGET' CHECK (purpose='KUBERNETES_TARGET'),
 secret_namespace text NOT NULL CHECK (secret_namespace ~ '^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$'),
 secret_name text NOT NULL CHECK (secret_name ~ '^kbi-[a-f0-9]{32}-r[1-9][0-9]{0,18}$'),
 secret_uid uuid NOT NULL CHECK (secret_uid<>'00000000-0000-0000-0000-000000000000'::uuid),
 secret_resource_version text NOT NULL CHECK (char_length(secret_resource_version) BETWEEN 1 AND 128),
 content_sha256 text NOT NULL CHECK (content_sha256 ~ '^[a-f0-9]{64}$'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 FOREIGN KEY (organization_id,identity_revision_id,purpose)
  REFERENCES control_plane.integration_broker_identity_revisions(organization_id,id,purpose),
 UNIQUE (secret_namespace,secret_name),
 UNIQUE (secret_uid)
);
CREATE TRIGGER kubernetes_identity_material_revisions_immutable
 BEFORE UPDATE OR DELETE ON control_plane.kubernetes_identity_material_revisions
 FOR EACH ROW EXECUTE FUNCTION control_plane.reject_integration_immutable_update();

REVOKE ALL ON control_plane.integration_broker_identity_revisions,
 control_plane.kubernetes_identity_material_revisions FROM control_plane_runtime;
GRANT SELECT,INSERT ON control_plane.integration_broker_identity_revisions,
 control_plane.kubernetes_identity_material_revisions TO control_plane_runtime;

-- Общий personal test lifecycle сохраняется; source имеет явный неизменяемый oneof.
-- Старые TOKEN rows известны из прежнего NOT NULL/FK контракта, не из отсутствия descriptor.
ALTER TABLE control_plane.integration_personal_credential_tests
 ALTER COLUMN credential_revision_id DROP NOT NULL,
 ADD COLUMN credential_kind text NOT NULL DEFAULT 'TOKEN' CHECK(credential_kind IN('TOKEN','BROKER_IDENTITY')),
 ADD COLUMN broker_purpose text CHECK(broker_purpose IN('KUBERNETES_TARGET','SSH_TARGET')),
 ADD COLUMN broker_identity_revision_id uuid,
 ADD CONSTRAINT personal_test_source_oneof CHECK(
  (credential_kind='TOKEN' AND credential_revision_id IS NOT NULL AND broker_identity_revision_id IS NULL AND broker_purpose IS NULL)
  OR (credential_kind='BROKER_IDENTITY' AND credential_revision_id IS NULL AND broker_identity_revision_id IS NOT NULL AND broker_purpose IS NOT NULL)),
 ADD CONSTRAINT personal_test_broker_identity_owner
  FOREIGN KEY(organization_id,binding_id,broker_purpose,broker_identity_revision_id)
  REFERENCES control_plane.integration_broker_identity_revisions(organization_id,binding_id,purpose,id);

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_personal_test_broker_source() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,control_plane AS $personal_test_source$
BEGIN
 IF TG_OP='UPDATE' AND (NEW.credential_kind,NEW.broker_purpose,NEW.broker_identity_revision_id)
  IS DISTINCT FROM (OLD.credential_kind,OLD.broker_purpose,OLD.broker_identity_revision_id) THEN
  RAISE EXCEPTION 'Personal credential test source is immutable';
 END IF;
 IF TG_OP='INSERT' AND NEW.credential_kind='BROKER_IDENTITY' AND NOT EXISTS(
  SELECT 1 FROM control_plane.integration_broker_identity_revisions identity
  JOIN control_plane.integration_personal_credentials binding ON binding.id=identity.binding_id
   AND binding.organization_id=identity.organization_id AND binding.connection_id=identity.connection_id
   AND binding.subject_id=identity.subject_id
  WHERE identity.id=NEW.broker_identity_revision_id AND identity.organization_id=NEW.organization_id
   AND identity.connection_id=NEW.connection_id AND identity.binding_id=NEW.binding_id AND identity.subject_id=NEW.subject_id
   AND identity.credential_policy='INITIATOR_REQUIRED' AND identity.purpose=NEW.broker_purpose
   AND binding.credential_kind=NEW.credential_kind AND binding.broker_purpose=NEW.broker_purpose
   AND binding.broker_identity_revision_id=identity.id AND binding.state='CHECKING'
   AND NEW.input_snapshot->>'Kind'='BROKER_IDENTITY' AND NEW.input_snapshot->>'BrokerPurpose'=identity.purpose
   AND NEW.input_snapshot->>'BrokerIdentityRef'=identity.ref AND NEW.input_snapshot->>'CredentialRef'=''
   AND NEW.input_snapshot->>'BrokerTargetRef'=identity.target_ref
 ) THEN RAISE EXCEPTION 'Personal credential test broker source is invalid';END IF;
 RETURN NEW;
END $personal_test_source$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_personal_test_broker_source() FROM PUBLIC;
CREATE TRIGGER personal_credential_test_source BEFORE INSERT OR UPDATE
 ON control_plane.integration_personal_credential_tests FOR EACH ROW
 EXECUTE FUNCTION control_plane.guard_personal_test_broker_source();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.guard_personal_integration_credential() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,control_plane AS $personal_source$
DECLARE credential_changed boolean; newly_revoked boolean;
BEGIN
 IF TG_OP='DELETE' THEN
  RAISE EXCEPTION 'personal integration credential history cannot be deleted' USING ERRCODE='23514';
 END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.state<>'NOT_CONFIGURED' OR NEW.version<>1 OR NEW.generation<>1
   OR NEW.credential_revision_id IS NOT NULL OR NEW.broker_identity_revision_id IS NOT NULL THEN
   RAISE EXCEPTION 'personal integration credential initial state is invalid' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF (NEW.id,NEW.ref,NEW.organization_id,NEW.connection_id,NEW.subject_id,NEW.created_at,NEW.credential_kind,NEW.broker_purpose)
  IS DISTINCT FROM
  (OLD.id,OLD.ref,OLD.organization_id,OLD.connection_id,OLD.subject_id,OLD.created_at,OLD.credential_kind,OLD.broker_purpose)
  OR OLD.version=9223372036854775807 OR NEW.version<>OLD.version+1 THEN
  RAISE EXCEPTION 'personal integration credential owner or version is invalid' USING ERRCODE='23514';
 END IF;
 credential_changed:=(NEW.credential_revision_id,NEW.broker_identity_revision_id)
  IS DISTINCT FROM (OLD.credential_revision_id,OLD.broker_identity_revision_id);
 newly_revoked:=NEW.state='REVOKED' AND OLD.state<>'REVOKED';
 IF credential_changed OR newly_revoked THEN
  IF OLD.generation=9223372036854775807 OR NEW.generation<>OLD.generation+1
   OR NEW.state NOT IN ('CHECK_REQUIRED','REVOKED')
   OR (credential_changed AND (NEW.state<>'CHECK_REQUIRED' OR num_nonnulls(NEW.credential_revision_id,NEW.broker_identity_revision_id)<>1)) THEN
   RAISE EXCEPTION 'personal integration credential generation is invalid' USING ERRCODE='23514';
  END IF;
  IF credential_changed AND OLD.credential_revision_id IS NOT NULL AND NOT EXISTS (
   SELECT 1 FROM control_plane.integration_personal_credential_revisions previous_revision
   JOIN control_plane.integration_personal_credential_revisions next_revision
    ON next_revision.binding_id=previous_revision.binding_id AND next_revision.organization_id=previous_revision.organization_id
     AND next_revision.revision>previous_revision.revision
   WHERE previous_revision.id=OLD.credential_revision_id AND next_revision.id=NEW.credential_revision_id
    AND previous_revision.binding_id=OLD.id AND previous_revision.organization_id=OLD.organization_id
  ) THEN RAISE EXCEPTION 'personal integration credential revision must advance' USING ERRCODE='23514';END IF;
  IF credential_changed AND OLD.broker_identity_revision_id IS NOT NULL AND NOT EXISTS (
   SELECT 1 FROM control_plane.integration_broker_identity_revisions previous_revision
   JOIN control_plane.integration_broker_identity_revisions next_revision
    ON next_revision.binding_id=previous_revision.binding_id AND next_revision.organization_id=previous_revision.organization_id
     AND next_revision.purpose=previous_revision.purpose AND next_revision.revision>previous_revision.revision
   WHERE previous_revision.id=OLD.broker_identity_revision_id AND next_revision.id=NEW.broker_identity_revision_id
    AND previous_revision.binding_id=OLD.id AND previous_revision.organization_id=OLD.organization_id
  ) THEN RAISE EXCEPTION 'personal broker identity revision must advance' USING ERRCODE='23514';END IF;
 ELSIF NEW.generation<>OLD.generation OR NOT (
  (OLD.state IN ('CHECK_REQUIRED','READY','INVALID') AND NEW.state='CHECKING')
  OR (OLD.state IN ('READY','INVALID') AND NEW.state='CHECK_REQUIRED')
  OR (OLD.state='CHECKING' AND NEW.state IN ('READY','INVALID','CHECK_REQUIRED'))
 ) THEN RAISE EXCEPTION 'personal integration credential transition is invalid' USING ERRCODE='23514';END IF;
 NEW.updated_at:=clock_timestamp();
 RETURN NEW;
END $personal_source$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_personal_integration_credential() FROM PUBLIC;

-- Forward extension существующего Connection lifecycle: все version-pinned
-- setup grants закрываются атомарно, а READY BI очищает свой verified source.
-- TOKEN history и неопределённые material observations не переписываются.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.cancel_personal_tests_on_connection_change() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,control_plane AS $connection_source$
BEGIN
 IF NEW.version IS DISTINCT FROM OLD.version THEN
  WITH selected AS MATERIALIZED (
   SELECT setup.id FROM control_plane.kubernetes_identity_setups setup
   WHERE setup.connection_id=NEW.id AND setup.organization_id=NEW.organization_id
    AND setup.state IN('PREPARED','ATTEMPTING','UNKNOWN')
   ORDER BY setup.id FOR UPDATE
  ), closed_attempts AS (
   UPDATE control_plane.kubernetes_identity_upload_attempts attempt
   SET state='CANCELLED',closed_at=clock_timestamp()
   FROM selected WHERE attempt.setup_id=selected.id AND attempt.organization_id=NEW.organization_id
    AND attempt.state='ACTIVE' RETURNING attempt.id
  )
  UPDATE control_plane.kubernetes_identity_setups setup SET state='CANCELLED',version=version+1
  FROM selected WHERE setup.id=selected.id AND setup.organization_id=NEW.organization_id;
  UPDATE control_plane.integration_personal_credential_tests
  SET state='CANCELLED',outcome='CANCELLED',version=version+1,finished_at=clock_timestamp(),
   lease_ref=NULL,fence_digest=NULL,workload_instance=NULL,lease_expires_at=NULL
  WHERE connection_id=NEW.id AND state IN('DUE','CLAIMED','PROBING');
  UPDATE control_plane.integration_personal_credentials
  SET state='CHECK_REQUIRED',version=version+1 WHERE connection_id=NEW.id AND state='CHECKING';
  IF (NEW.public_configuration,NEW.definition_version,NEW.definition_digest,NEW.credential_policy)
   IS DISTINCT FROM (OLD.public_configuration,OLD.definition_version,OLD.definition_digest,OLD.credential_policy) THEN
   UPDATE control_plane.integration_personal_credentials
   SET state='CHECK_REQUIRED',version=version+1,verified_credential_revision_id=NULL,verified_broker_identity_revision_id=NULL,
    verified_definition_digest=NULL,verified_configuration_digest=NULL,verified_capabilities='{}',denied_capabilities='{}',
    observed_permission_profiles='{}',expires_at=NULL
   WHERE connection_id=NEW.id AND state IN('READY','INVALID');
  END IF;
 END IF;
 RETURN NEW;
END $connection_source$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.cancel_personal_tests_on_connection_change() FROM PUBLIC;

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_kubernetes_identity_material() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,control_plane AS $purpose_material$
BEGIN
 IF NOT EXISTS (
  SELECT 1 FROM control_plane.integration_broker_identity_revisions identity
  WHERE identity.id=NEW.identity_revision_id AND identity.organization_id=NEW.organization_id
   AND identity.purpose='KUBERNETES_TARGET'
   AND NEW.secret_name='kbi-'||substr(encode(public.digest(convert_to(identity.ref,'UTF8'),'sha256'),'hex'),1,32)||'-r'||identity.revision::text
 ) THEN RAISE EXCEPTION 'Kubernetes identity material purpose or name changed';END IF;
 RETURN NEW;
END $purpose_material$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_kubernetes_identity_material() FROM PUBLIC;
CREATE TRIGGER kubernetes_identity_material_owner
 BEFORE INSERT ON control_plane.kubernetes_identity_material_revisions
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_kubernetes_identity_material();

-- UNKNOWN receipt append-only: поздний KNOWN факт дополняет, но не переписывает
-- прежний outcome и сам по себе не возвращает ни setup grant, ни binding.
CREATE TABLE control_plane.kubernetes_identity_upload_observations (
 attempt_id uuid NOT NULL REFERENCES control_plane.kubernetes_identity_upload_attempts(id),
 outcome text NOT NULL CHECK (outcome IN('UNKNOWN','KNOWN')),
 identity_revision_id uuid REFERENCES control_plane.kubernetes_identity_material_revisions(identity_revision_id),
 observed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY (attempt_id,outcome),
 CHECK ((outcome='UNKNOWN' AND identity_revision_id IS NULL) OR (outcome='KNOWN' AND identity_revision_id IS NOT NULL))
);
-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_kubernetes_identity_upload_observation() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,control_plane AS $setup_observation$
BEGIN
 PERFORM setup.id FROM control_plane.kubernetes_identity_setups setup
 JOIN control_plane.kubernetes_identity_upload_attempts attempt ON attempt.setup_id=setup.id
 WHERE attempt.id=NEW.attempt_id FOR UPDATE OF setup;
 IF EXISTS(SELECT 1 FROM control_plane.kubernetes_identity_upload_attempts original
  JOIN control_plane.kubernetes_identity_upload_attempts rejected ON rejected.setup_id=original.setup_id
  JOIN control_plane.kubernetes_identity_acknowledgement_rejections rejection ON rejection.attempt_id=rejected.id
  WHERE original.id=NEW.attempt_id) THEN
  RAISE EXCEPTION 'Rejected Kubernetes acknowledgement cannot be reclassified';
 END IF;
 IF NEW.outcome='KNOWN' AND NOT EXISTS (
  SELECT 1 FROM control_plane.kubernetes_identity_upload_attempts attempt
  JOIN control_plane.kubernetes_identity_setups setup ON setup.id=attempt.setup_id
  JOIN control_plane.kubernetes_identity_material_revisions material
   ON material.identity_revision_id=setup.identity_revision_id
   AND material.organization_id=setup.organization_id AND material.content_sha256=setup.content_sha256
  JOIN control_plane.kubernetes_operator_registry_revisions registry ON registry.revision=setup.registry_revision
  WHERE attempt.id=NEW.attempt_id AND setup.identity_revision_id=NEW.identity_revision_id
   AND material.secret_namespace=registry.identity_namespace
   AND NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_identity_acknowledgement_rejections rejection
    JOIN control_plane.kubernetes_identity_upload_attempts rejected_attempt ON rejected_attempt.id=rejection.attempt_id
    WHERE rejected_attempt.setup_id=setup.id)
 ) THEN RAISE EXCEPTION 'Kubernetes identity observation owner boundary changed';END IF;
 RETURN NEW;
END $setup_observation$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_kubernetes_identity_upload_observation() FROM PUBLIC;
CREATE TRIGGER kubernetes_identity_upload_observation_owner BEFORE INSERT
 ON control_plane.kubernetes_identity_upload_observations FOR EACH ROW
 EXECUTE FUNCTION control_plane.guard_kubernetes_identity_upload_observation();
CREATE TRIGGER kubernetes_identity_upload_observations_immutable BEFORE UPDATE OR DELETE
 ON control_plane.kubernetes_identity_upload_observations FOR EACH ROW
 EXECUTE FUNCTION control_plane.reject_integration_immutable_update();
REVOKE ALL ON control_plane.kubernetes_identity_upload_observations FROM control_plane_runtime;
GRANT SELECT,INSERT ON control_plane.kubernetes_identity_upload_observations TO control_plane_runtime;

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_kubernetes_identity_acknowledgement_rejection() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,control_plane AS $setup_acknowledgement$
BEGIN
 PERFORM setup.id FROM control_plane.kubernetes_identity_setups setup
 JOIN control_plane.kubernetes_identity_upload_attempts attempt ON attempt.setup_id=setup.id
 WHERE attempt.id=NEW.attempt_id FOR UPDATE OF setup;
 NEW.conflicts_with_known:=EXISTS(SELECT 1 FROM control_plane.kubernetes_identity_upload_attempts original
  JOIN control_plane.kubernetes_identity_upload_attempts observed ON observed.setup_id=original.setup_id
  JOIN control_plane.kubernetes_identity_upload_observations observation ON observation.attempt_id=observed.id AND observation.outcome='KNOWN'
  WHERE original.id=NEW.attempt_id);
 RETURN NEW;
END $setup_acknowledgement$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_kubernetes_identity_acknowledgement_rejection() FROM PUBLIC;
CREATE TRIGGER kubernetes_identity_acknowledgement_owner BEFORE INSERT
 ON control_plane.kubernetes_identity_acknowledgement_rejections FOR EACH ROW
 EXECUTE FUNCTION control_plane.guard_kubernetes_identity_acknowledgement_rejection();

-- Deferred closure разрешает один owner transaction, но не partial terminal envelope.
-- Expired ACTIVE не считается закрытым лишь по timestamp: нужен явный transition.
-- +goose StatementBegin
CREATE FUNCTION control_plane.check_kubernetes_identity_setup_closure() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,control_plane AS $setup_closure$
DECLARE pinned_setup_id uuid; terminal_state text;
BEGIN
 IF TG_TABLE_NAME='kubernetes_identity_setups' THEN pinned_setup_id:=NEW.id;
 ELSIF TG_TABLE_NAME='kubernetes_identity_upload_attempts' THEN pinned_setup_id:=NEW.setup_id;
 ELSIF TG_TABLE_NAME='kubernetes_identity_acknowledgement_rejections' THEN
  SELECT setup_id INTO pinned_setup_id FROM control_plane.kubernetes_identity_upload_attempts WHERE id=NEW.attempt_id;
 ELSE RAISE EXCEPTION 'Kubernetes identity closure source is invalid';END IF;
 SELECT state INTO terminal_state FROM control_plane.kubernetes_identity_setups WHERE id=pinned_setup_id;
 IF terminal_state IN('COMPLETED','CANCELLED','EXPIRED','ACK_REJECTED') AND EXISTS(
  SELECT 1 FROM control_plane.kubernetes_identity_upload_attempts attempt
  WHERE attempt.setup_id=pinned_setup_id AND attempt.state='ACTIVE'
 ) THEN RAISE EXCEPTION 'Kubernetes identity terminal setup retains active attempts';END IF;
 IF terminal_state='COMPLETED' AND NOT EXISTS(
  SELECT 1 FROM control_plane.kubernetes_identity_upload_attempts attempt
  JOIN control_plane.kubernetes_identity_upload_observations observation ON observation.attempt_id=attempt.id
  WHERE attempt.setup_id=pinned_setup_id AND observation.outcome='KNOWN'
 ) THEN RAISE EXCEPTION 'Kubernetes identity completion lacks material evidence';END IF;
 IF terminal_state='COMPLETED' AND EXISTS(
  SELECT 1 FROM control_plane.kubernetes_identity_upload_attempts attempt WHERE attempt.setup_id=pinned_setup_id
   AND NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_identity_upload_observations observation WHERE observation.attempt_id=attempt.id)
 ) THEN RAISE EXCEPTION 'Kubernetes identity completion retained an unreported outcome';END IF;
 IF terminal_state='ACK_REJECTED' AND NOT EXISTS(
  SELECT 1 FROM control_plane.kubernetes_identity_acknowledgement_rejections rejection
  JOIN control_plane.kubernetes_identity_upload_attempts attempt ON attempt.id=rejection.attempt_id
  WHERE attempt.setup_id=pinned_setup_id
 ) THEN RAISE EXCEPTION 'Kubernetes identity rejection lacks acknowledgement evidence';END IF;
 IF EXISTS(SELECT 1 FROM control_plane.kubernetes_identity_upload_attempts attempt
  WHERE attempt.setup_id=pinned_setup_id AND attempt.state='ACK_REJECTED' AND NOT EXISTS(
   SELECT 1 FROM control_plane.kubernetes_identity_acknowledgement_rejections rejection WHERE rejection.attempt_id=attempt.id
  )) THEN RAISE EXCEPTION 'Kubernetes attempt rejection lacks acknowledgement evidence';END IF;
 IF EXISTS(SELECT 1 FROM control_plane.kubernetes_identity_acknowledgement_rejections rejection
  JOIN control_plane.kubernetes_identity_upload_attempts attempt ON attempt.id=rejection.attempt_id
  WHERE attempt.setup_id=pinned_setup_id) AND terminal_state NOT IN('COMPLETED','CANCELLED','EXPIRED','ACK_REJECTED') THEN
  RAISE EXCEPTION 'Rejected Kubernetes acknowledgement retained setup authority';
 END IF;
 IF terminal_state='COMPLETED' AND EXISTS(
  SELECT 1 FROM control_plane.kubernetes_identity_acknowledgement_rejections rejection
  JOIN control_plane.kubernetes_identity_upload_attempts attempt ON attempt.id=rejection.attempt_id
  WHERE attempt.setup_id=pinned_setup_id AND NOT rejection.conflicts_with_known
 ) THEN RAISE EXCEPTION 'Rejected Kubernetes acknowledgement became completed';END IF;
 IF EXISTS(SELECT 1 FROM control_plane.kubernetes_identity_acknowledgement_rejections rejection
  JOIN control_plane.kubernetes_identity_upload_attempts attempt ON attempt.id=rejection.attempt_id
  JOIN control_plane.kubernetes_identity_setups setup ON setup.id=attempt.setup_id
  JOIN control_plane.integration_personal_credentials binding ON binding.broker_identity_revision_id=setup.identity_revision_id
   AND binding.organization_id=setup.organization_id
  WHERE setup.id=pinned_setup_id AND binding.state<>'REVOKED'
 ) THEN RAISE EXCEPTION 'Rejected Kubernetes acknowledgement retained current personal authority';END IF;
 IF EXISTS(SELECT 1 FROM control_plane.kubernetes_identity_acknowledgement_rejections rejection
  JOIN control_plane.kubernetes_identity_upload_attempts attempt ON attempt.id=rejection.attempt_id
  JOIN control_plane.kubernetes_identity_setups setup ON setup.id=attempt.setup_id
  JOIN control_plane.integration_connections connection ON connection.broker_identity_revision_id=setup.identity_revision_id
   AND connection.organization_id=setup.organization_id
  WHERE setup.id=pinned_setup_id AND (connection.state NOT IN('NOT_CONNECTED','DISABLED') OR connection.last_tested_at IS NOT NULL OR connection.last_test_summary<>'')
 ) THEN RAISE EXCEPTION 'Rejected Kubernetes acknowledgement retained current shared authority';END IF;
 RETURN NULL;
END $setup_closure$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.check_kubernetes_identity_setup_closure() FROM PUBLIC;
CREATE CONSTRAINT TRIGGER kubernetes_identity_setup_terminal AFTER INSERT OR UPDATE
 ON control_plane.kubernetes_identity_setups DEFERRABLE INITIALLY DEFERRED
 FOR EACH ROW EXECUTE FUNCTION control_plane.check_kubernetes_identity_setup_closure();
CREATE CONSTRAINT TRIGGER kubernetes_identity_attempt_terminal AFTER INSERT OR UPDATE
 ON control_plane.kubernetes_identity_upload_attempts DEFERRABLE INITIALLY DEFERRED
 FOR EACH ROW EXECUTE FUNCTION control_plane.check_kubernetes_identity_setup_closure();
CREATE CONSTRAINT TRIGGER kubernetes_identity_acknowledgement_terminal AFTER INSERT
 ON control_plane.kubernetes_identity_acknowledgement_rejections DEFERRABLE INITIALLY DEFERRED
 FOR EACH ROW EXECUTE FUNCTION control_plane.check_kubernetes_identity_setup_closure();

-- Health хранит отдельный immutable admission: потеря ACK не разрешает повторный probe.
CREATE TABLE control_plane.kubernetes_identity_health_attempts (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 ref text NOT NULL UNIQUE CHECK(char_length(ref) BETWEEN 8 AND 96),
 organization_id uuid NOT NULL REFERENCES control_plane.organizations(id) DEFERRABLE INITIALLY IMMEDIATE,
 personal_test_id uuid NOT NULL UNIQUE REFERENCES control_plane.integration_personal_credential_tests(id) DEFERRABLE INITIALLY IMMEDIATE,
 identity_revision_id uuid NOT NULL REFERENCES control_plane.kubernetes_identity_material_revisions(identity_revision_id) DEFERRABLE INITIALLY IMMEDIATE,
 registry_revision bigint NOT NULL REFERENCES control_plane.kubernetes_operator_registry_revisions(revision) DEFERRABLE INITIALLY IMMEDIATE,
 target_ref text NOT NULL,
 target_revision bigint NOT NULL CHECK(target_revision>0),
 snapshot_digest text NOT NULL CHECK(snapshot_digest ~ '^[a-f0-9]{64}$'),
 input_digest text NOT NULL CHECK(input_digest ~ '^[a-f0-9]{64}$'),
 lease_ref text NOT NULL,
 fence_digest text NOT NULL CHECK(fence_digest ~ '^[a-f0-9]{64}$'),
 generation bigint NOT NULL CHECK(generation=1),
 workload_instance text NOT NULL CHECK(char_length(workload_instance) BETWEEN 1 AND 256),
 expires_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK(expires_at>created_at AND expires_at<=created_at+interval '30 seconds'),
 UNIQUE(organization_id,id)
);
CREATE TABLE control_plane.kubernetes_identity_health_completions (
 attempt_id uuid PRIMARY KEY REFERENCES control_plane.kubernetes_identity_health_attempts(id) DEFERRABLE INITIALLY IMMEDIATE,
 outcome text NOT NULL CHECK(outcome IN('AUTHENTICATED','AUTH_REJECTED','UNAVAILABLE')),
 subject_digest text,
 state text NOT NULL CHECK(state IN('SUCCEEDED','FAILED','CANCELLED')),
 applied boolean NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK((outcome='AUTHENTICATED' AND subject_digest IS NOT NULL AND subject_digest ~ '^[a-f0-9]{64}$') OR
       (outcome<>'AUTHENTICATED' AND subject_digest IS NULL))
);
CREATE TRIGGER kubernetes_identity_health_attempt_immutable BEFORE UPDATE OR DELETE
 ON control_plane.kubernetes_identity_health_attempts FOR EACH ROW
 EXECUTE FUNCTION control_plane.reject_integration_immutable_update();
CREATE TRIGGER kubernetes_identity_health_completion_immutable BEFORE UPDATE OR DELETE
 ON control_plane.kubernetes_identity_health_completions FOR EACH ROW
 EXECUTE FUNCTION control_plane.reject_integration_immutable_update();
REVOKE ALL ON control_plane.kubernetes_identity_health_attempts,
 control_plane.kubernetes_identity_health_completions FROM control_plane_runtime;
GRANT SELECT,INSERT ON control_plane.kubernetes_identity_health_attempts,
 control_plane.kubernetes_identity_health_completions TO control_plane_runtime;

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_kubernetes_identity_health() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,control_plane AS $health_guard$
BEGIN
 IF TG_TABLE_NAME='kubernetes_identity_health_attempts' THEN
  IF NOT EXISTS(SELECT 1 FROM control_plane.integration_personal_credential_tests test
   JOIN control_plane.integration_personal_credentials binding ON binding.id=test.binding_id
    AND binding.organization_id=test.organization_id AND binding.subject_id=test.subject_id
   JOIN control_plane.integration_broker_identity_revisions identity ON identity.id=NEW.identity_revision_id
    AND identity.organization_id=test.organization_id AND identity.binding_id=binding.id
    AND identity.subject_id=test.subject_id AND identity.connection_id=test.connection_id
   JOIN control_plane.kubernetes_operator_current current_source ON current_source.singleton
    AND current_source.revision=NEW.registry_revision
   JOIN control_plane.kubernetes_target_revisions target ON target.registry_revision<=NEW.registry_revision
    AND target.ref=NEW.target_ref AND target.revision=NEW.target_revision
   WHERE test.id=NEW.personal_test_id AND test.organization_id=NEW.organization_id
    AND test.state='CLAIMED' AND test.credential_kind='BROKER_IDENTITY' AND test.broker_purpose='KUBERNETES_TARGET'
    AND test.broker_identity_revision_id=identity.id AND binding.broker_identity_revision_id=identity.id
    AND binding.state='CHECKING' AND binding.credential_kind='BROKER_IDENTITY' AND binding.broker_purpose='KUBERNETES_TARGET'
    AND identity.purpose='KUBERNETES_TARGET' AND identity.target_ref=NEW.target_ref
    AND test.lease_ref=NEW.lease_ref AND test.fence_digest=NEW.fence_digest AND test.generation=NEW.generation
    AND test.workload_instance=NEW.workload_instance AND test.lease_expires_at=NEW.expires_at
    AND test.lease_expires_at>clock_timestamp()
  ) THEN RAISE EXCEPTION 'Managed Kubernetes health admission is not current'; END IF;
 ELSE
  IF NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_identity_health_attempts attempt
   JOIN control_plane.integration_personal_credential_tests test ON test.id=attempt.personal_test_id
    AND test.organization_id=attempt.organization_id
   WHERE attempt.id=NEW.attempt_id AND (
    (NOT NEW.applied AND NEW.state='CANCELLED') OR
    (NEW.applied AND attempt.expires_at>clock_timestamp() AND test.state=NEW.state AND
     ((NEW.outcome='AUTHENTICATED' AND NEW.state='SUCCEEDED') OR (NEW.outcome<>'AUTHENTICATED' AND NEW.state='FAILED')))
   )
  ) THEN RAISE EXCEPTION 'Managed Kubernetes health completion is not closed'; END IF;
 END IF;
 RETURN NEW;
END $health_guard$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_kubernetes_identity_health() FROM PUBLIC;
CREATE TRIGGER kubernetes_identity_health_admission_owner BEFORE INSERT
 ON control_plane.kubernetes_identity_health_attempts FOR EACH ROW
 EXECUTE FUNCTION control_plane.guard_kubernetes_identity_health();
CREATE TRIGGER kubernetes_identity_health_completion_owner BEFORE INSERT
 ON control_plane.kubernetes_identity_health_completions FOR EACH ROW
 EXECUTE FUNCTION control_plane.guard_kubernetes_identity_health();

-- Только Kubernetes получает несколько exact namespace scopes для одного recipient/capability.
-- Прежняя уникальность остальных adapters сохраняется отдельным partial index.
-- +goose StatementBegin
DO $grant_unique$
DECLARE previous_name text; previous_count integer;
BEGIN
 SELECT count(*),min(conname) INTO previous_count,previous_name FROM pg_catalog.pg_constraint
 WHERE conrelid='control_plane.integration_grants'::regclass AND contype='u'
  AND pg_catalog.pg_get_constraintdef(oid)='UNIQUE (connection_id, capability_key, target_kind, target_ref)';
 IF previous_count<>1 THEN RAISE EXCEPTION 'Integration grant unique predecessor changed'; END IF;
 EXECUTE pg_catalog.format('ALTER TABLE control_plane.integration_grants DROP CONSTRAINT %I',previous_name);
END $grant_unique$;
-- +goose StatementEnd
CREATE UNIQUE INDEX integration_grants_ordinary_target_unique ON control_plane.integration_grants
 (connection_id,capability_key,target_kind,target_ref) WHERE resource_kind<>'KUBERNETES_TARGET';
CREATE UNIQUE INDEX integration_grants_kubernetes_scope_unique ON control_plane.integration_grants
 (connection_id,capability_key,target_kind,target_ref,resource_scope_digest) WHERE resource_kind='KUBERNETES_TARGET';

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_kubernetes_grant_scope() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,control_plane AS $grant_scope$
DECLARE namespace text; target text; canonical text;
BEGIN
 IF NEW.resource_kind<>'KUBERNETES_TARGET' THEN
  IF TG_OP='UPDATE' AND OLD.resource_kind='KUBERNETES_TARGET' THEN RAISE EXCEPTION 'Managed Kubernetes grant kind is immutable'; END IF;
  RETURN NEW;
 END IF;
 namespace:=NEW.resource_scope->>'namespace'; target:=NEW.resource_scope->>'target_ref';
 IF jsonb_typeof(NEW.resource_scope)<>'object' OR namespace IS NULL OR target IS NULL
  OR jsonb_typeof(NEW.resource_scope->'namespace')<>'string' OR jsonb_typeof(NEW.resource_scope->'target_ref')<>'string'
  OR NEW.resource_scope-'namespace'-'target_ref'<>'{}'::jsonb
  OR target !~ '^[A-Za-z0-9_-]{8,96}$'
  OR NOT(namespace IN('@cluster','@all-namespaces') OR namespace ~ '^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$')
  OR ((namespace='@all-namespaces') IS DISTINCT FROM (NEW.capability_key IN('kubernetes.list.all-namespaces','kubernetes.watch.all-namespaces')))
  OR NOT control_plane.integration_declared_policy_allowed(NEW.connection_id,NEW.capability_key,NEW.capability_key,NEW.risk,NEW.resource_kind,NEW.approval_policy,'{}'::jsonb)
  OR NOT EXISTS(SELECT 1 FROM control_plane.integration_connections connection
   WHERE connection.id=NEW.connection_id AND connection.organization_id=NEW.organization_id
    AND connection.definition_key='kubernetes-managed' AND connection.public_configuration->>'target_ref'=target)
 THEN RAISE EXCEPTION 'Managed Kubernetes grant scope is invalid'; END IF;
 canonical:='{"namespace":'||to_json(namespace)::text||',"target_ref":'||to_json(target)::text||'}';
 IF NEW.resource_scope_digest IS DISTINCT FROM encode(public.digest(convert_to(canonical,'UTF8'),'sha256'),'hex')
 THEN RAISE EXCEPTION 'Managed Kubernetes grant scope digest is invalid'; END IF;
 IF TG_OP='UPDATE' AND (NEW.resource_scope IS DISTINCT FROM OLD.resource_scope OR NEW.resource_scope_digest IS DISTINCT FROM OLD.resource_scope_digest
  OR NEW.connection_id IS DISTINCT FROM OLD.connection_id OR NEW.capability_key IS DISTINCT FROM OLD.capability_key
  OR NEW.target_kind IS DISTINCT FROM OLD.target_kind OR NEW.target_ref IS DISTINCT FROM OLD.target_ref) THEN
  RAISE EXCEPTION 'Managed Kubernetes grant scope is immutable'; END IF;
 RETURN NEW;
END $grant_scope$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_kubernetes_grant_scope() FROM PUBLIC;
CREATE TRIGGER kubernetes_grant_scope_owner BEFORE INSERT OR UPDATE ON control_plane.integration_grants
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_kubernetes_grant_scope();

-- Позиция discovery не несёт authority, grant либо reusable execution credit.
CREATE TABLE control_plane.kubernetes_invocation_claim_cursors (
 organization_id uuid PRIMARY KEY REFERENCES control_plane.organizations(id) DEFERRABLE INITIALLY IMMEDIATE,
 after_created_at timestamptz NOT NULL DEFAULT 'epoch',
 after_ref text NOT NULL DEFAULT '',
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK((after_created_at='epoch' AND after_ref='') OR
  (after_created_at>'epoch' AND after_ref ~ '^[A-Za-z0-9_-]{8,96}$'))
);
REVOKE ALL ON control_plane.kubernetes_invocation_claim_cursors FROM control_plane_runtime;
GRANT SELECT,INSERT,UPDATE ON control_plane.kubernetes_invocation_claim_cursors TO control_plane_runtime;

-- Invocation source принадлежит original root и не заменяется после смены личной identity.
CREATE TABLE control_plane.kubernetes_invocation_identity_bindings (
 invocation_id uuid PRIMARY KEY REFERENCES control_plane.integration_invocations(id) DEFERRABLE INITIALLY IMMEDIATE,
 organization_id uuid NOT NULL REFERENCES control_plane.organizations(id) DEFERRABLE INITIALLY IMMEDIATE,
 connection_id uuid NOT NULL REFERENCES control_plane.integration_connections(id) DEFERRABLE INITIALLY IMMEDIATE,
 root_run_id uuid NOT NULL REFERENCES control_plane.runs(id) DEFERRABLE INITIALLY IMMEDIATE,
 root_actor_id uuid NOT NULL REFERENCES control_plane.subjects(id) DEFERRABLE INITIALLY IMMEDIATE,
 subject_id uuid NOT NULL REFERENCES control_plane.subjects(id) DEFERRABLE INITIALLY IMMEDIATE,
 binding_id uuid REFERENCES control_plane.integration_personal_credentials(id) DEFERRABLE INITIALLY IMMEDIATE,
 binding_generation bigint NOT NULL CHECK(binding_generation>=0),
 identity_revision_id uuid NOT NULL REFERENCES control_plane.kubernetes_identity_material_revisions(identity_revision_id) DEFERRABLE INITIALLY IMMEDIATE,
 health_attempt_id uuid NOT NULL REFERENCES control_plane.kubernetes_identity_health_completions(attempt_id) DEFERRABLE INITIALLY IMMEDIATE,
 credential_policy text NOT NULL CHECK(credential_policy IN('INITIATOR_REQUIRED','PROJECT_SHARED')),
 definition_digest text NOT NULL CHECK(definition_digest ~ '^[a-f0-9]{64}$'),
 configuration_digest text NOT NULL CHECK(configuration_digest ~ '^[a-f0-9]{64}$'),
 registry_revision bigint NOT NULL REFERENCES control_plane.kubernetes_operator_registry_revisions(revision) DEFERRABLE INITIALLY IMMEDIATE,
 target_ref text NOT NULL,
 target_revision bigint NOT NULL,
 subject_digest text NOT NULL CHECK(subject_digest ~ '^[a-f0-9]{64}$'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 FOREIGN KEY(target_ref,target_revision) REFERENCES control_plane.kubernetes_target_revisions(ref,revision) DEFERRABLE INITIALLY IMMEDIATE,
 CHECK((credential_policy='INITIATOR_REQUIRED' AND binding_id IS NOT NULL AND binding_generation>0 AND subject_id=root_actor_id)
  OR (credential_policy='PROJECT_SHARED' AND binding_id IS NULL AND binding_generation=0))
);
CREATE TRIGGER kubernetes_invocation_identity_binding_immutable BEFORE UPDATE OR DELETE
 ON control_plane.kubernetes_invocation_identity_bindings FOR EACH ROW
 WHEN(NOT control_plane.project_purge_authorized())
 EXECUTE FUNCTION control_plane.reject_integration_immutable_update();
REVOKE ALL ON control_plane.kubernetes_invocation_identity_bindings FROM control_plane_runtime;
GRANT SELECT,INSERT ON control_plane.kubernetes_invocation_identity_bindings TO control_plane_runtime;

-- Runtime не может вставить чужую purpose lineage даже через прямой SQL consumer.
-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_kubernetes_invocation_identity() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,control_plane AS $invocation_guard$
BEGIN
 IF NOT EXISTS (
  SELECT 1 FROM control_plane.integration_invocations invocation
  JOIN control_plane.integration_connections connection ON connection.id=invocation.connection_id
   AND connection.organization_id=invocation.organization_id
  JOIN control_plane.runs execution ON execution.id=invocation.run_id AND execution.organization_id=invocation.organization_id
  JOIN control_plane.runs root ON root.id=execution.root_run_id AND root.organization_id=execution.organization_id
  JOIN control_plane.subjects actor ON actor.id=root.initiated_by AND actor.organization_id=root.organization_id AND actor.active
  JOIN control_plane.integration_broker_identity_revisions identity ON identity.id=NEW.identity_revision_id
   AND identity.organization_id=NEW.organization_id AND identity.connection_id=connection.id
   AND identity.purpose='KUBERNETES_TARGET' AND identity.target_ref=NEW.target_ref
   AND identity.credential_policy=connection.credential_policy AND identity.subject_id=NEW.subject_id
  JOIN control_plane.kubernetes_identity_health_attempts attempt ON attempt.id=NEW.health_attempt_id
   AND attempt.organization_id=NEW.organization_id AND attempt.identity_revision_id=identity.id
   AND attempt.registry_revision=NEW.registry_revision AND attempt.target_ref=NEW.target_ref AND attempt.target_revision=NEW.target_revision
  JOIN control_plane.kubernetes_identity_health_completions completion ON completion.attempt_id=attempt.id
   AND completion.outcome='AUTHENTICATED' AND completion.state='SUCCEEDED' AND completion.applied
   AND completion.subject_digest=NEW.subject_digest
  JOIN control_plane.kubernetes_operator_current current ON current.revision=NEW.registry_revision
  WHERE invocation.id=NEW.invocation_id AND invocation.organization_id=NEW.organization_id
   AND connection.id=NEW.connection_id AND root.id=NEW.root_run_id AND root.initiated_by=NEW.root_actor_id
   AND root.state IN('RUNNING','WAITING_HUMAN') AND invocation.state IN('READY','WAITING_APPROVAL')
   AND connection.definition_key='kubernetes-managed' AND connection.lifecycle_state='ACTIVE' AND connection.enabled
   AND connection.definition_digest=NEW.definition_digest AND invocation.definition_digest=NEW.definition_digest
   AND connection.credential_policy=NEW.credential_policy AND connection.public_configuration->>'target_ref'=NEW.target_ref
   AND ((NEW.credential_policy='INITIATOR_REQUIRED' AND actor.kind='USER' AND NEW.subject_id=actor.id
    AND NOT connection.public_configuration ? 'service_subject_ref'
    AND EXISTS(SELECT 1 FROM control_plane.integration_personal_credentials binding
     WHERE binding.id=NEW.binding_id AND binding.organization_id=NEW.organization_id AND binding.connection_id=connection.id
      AND binding.subject_id=actor.id AND binding.generation=NEW.binding_generation AND binding.state='READY'
      AND binding.credential_kind='BROKER_IDENTITY' AND binding.broker_purpose='KUBERNETES_TARGET'
      AND binding.broker_identity_revision_id=identity.id AND binding.verified_broker_identity_revision_id=identity.id
      AND binding.verified_definition_digest=NEW.definition_digest AND binding.verified_configuration_digest=NEW.configuration_digest))
    OR (NEW.credential_policy='PROJECT_SHARED' AND connection.broker_identity_revision_id=identity.id AND connection.state='CONNECTED'
     AND EXISTS(SELECT 1 FROM control_plane.subjects service WHERE service.id=NEW.subject_id
      AND service.organization_id=NEW.organization_id AND service.active AND service.kind='SERVICE'
      AND service.ref=connection.public_configuration->>'service_subject_ref')))
 ) THEN RAISE EXCEPTION 'Managed Kubernetes invocation source is not current'; END IF;
 RETURN NEW;
END $invocation_guard$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_kubernetes_invocation_identity() FROM PUBLIC;
CREATE TRIGGER kubernetes_invocation_identity_owner BEFORE INSERT
 ON control_plane.kubernetes_invocation_identity_bindings FOR EACH ROW
 EXECUTE FUNCTION control_plane.guard_kubernetes_invocation_identity();

-- Сначала фиксируется конечный план, затем отдельная one-attempt попытка каждого
-- child. Ни plan, ни immutable child сами по себе не выдают внешних полномочий.
ALTER TABLE control_plane.kubernetes_invocation_identity_bindings
 ADD CONSTRAINT kubernetes_invocation_identity_tenant_key UNIQUE(invocation_id,organization_id);
ALTER TABLE control_plane.kubernetes_node_transport_pins
 ADD CONSTRAINT kubernetes_node_transport_reference_generation UNIQUE(ref,generation);
CREATE TABLE control_plane.kubernetes_execution_plans (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 ref text NOT NULL UNIQUE CHECK(ref ~ '^kpl_[A-Za-z0-9_-]{8,89}$'),
 organization_id uuid NOT NULL REFERENCES control_plane.organizations(id) DEFERRABLE INITIALLY IMMEDIATE,
 invocation_id uuid NOT NULL UNIQUE,
 binding_digest text NOT NULL CHECK(binding_digest ~ '^[a-f0-9]{64}$'),
 snapshot_digest text NOT NULL CHECK(snapshot_digest ~ '^[a-f0-9]{64}$'),
 proposal_digest text NOT NULL CHECK(proposal_digest ~ '^[a-f0-9]{64}$'),
 plan_digest text NOT NULL CHECK(plan_digest ~ '^[a-f0-9]{64}$'),
 child_count integer NOT NULL CHECK(child_count BETWEEN 0 AND 256),
 hard_deadline timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(id,organization_id),
 FOREIGN KEY(invocation_id,organization_id) REFERENCES control_plane.kubernetes_invocation_identity_bindings(invocation_id,organization_id) DEFERRABLE INITIALLY IMMEDIATE,
 CHECK(hard_deadline>created_at AND hard_deadline<=created_at+interval '600 seconds')
);
ALTER TABLE control_plane.integration_grants ADD CONSTRAINT integration_grant_kubernetes_tenant_key UNIQUE(id,organization_id);
CREATE TABLE control_plane.kubernetes_execution_children (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 ref text NOT NULL UNIQUE CHECK(ref ~ '^kch_[A-Za-z0-9_-]{8,89}$'),
 organization_id uuid NOT NULL REFERENCES control_plane.organizations(id) DEFERRABLE INITIALLY IMMEDIATE,
 plan_id uuid NOT NULL,
 ordinal integer NOT NULL CHECK(ordinal BETWEEN 0 AND 255),
 operation text NOT NULL CHECK(operation ~ '^kubernetes\.[a-z][a-z.-]{1,63}$'),
 risk text NOT NULL CHECK(risk IN('READ','WRITE','SENSITIVE','DESTRUCTIVE')),
 approval_policy text NOT NULL CHECK(approval_policy IN('NONE','HUMAN_SCOPED','HUMAN_EACH_EFFECT')),
 capability_key text NOT NULL CHECK(capability_key ~ '^kubernetes\.[a-z][a-z.-]{1,63}$'),
 grant_id uuid NOT NULL REFERENCES control_plane.integration_grants(id) DEFERRABLE INITIALLY IMMEDIATE,
 grant_version bigint NOT NULL CHECK(grant_version>0),
 grant_scope_digest text NOT NULL CHECK(grant_scope_digest ~ '^[a-f0-9]{64}$'),
 input_json text NOT NULL CHECK(octet_length(input_json) BETWEEN 2 AND 524288 AND jsonb_typeof(input_json::jsonb)='object'),
 input_digest text NOT NULL CHECK(input_digest ~ '^[a-f0-9]{64}$'),
 observation_json jsonb NOT NULL CHECK(jsonb_typeof(observation_json)='object' AND octet_length(observation_json::text)<=8192),
 node_pin_ref text,
 node_pin_generation bigint,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(id,organization_id),UNIQUE(plan_id,ordinal),
 FOREIGN KEY(plan_id,organization_id) REFERENCES control_plane.kubernetes_execution_plans(id,organization_id) DEFERRABLE INITIALLY IMMEDIATE,
 FOREIGN KEY(grant_id,organization_id) REFERENCES control_plane.integration_grants(id,organization_id) DEFERRABLE INITIALLY IMMEDIATE,
 FOREIGN KEY(node_pin_ref,node_pin_generation) REFERENCES control_plane.kubernetes_node_transport_pins(ref,generation) DEFERRABLE INITIALLY IMMEDIATE,
 CHECK((risk='READ' AND approval_policy='NONE') OR(risk<>'READ' AND approval_policy<>'NONE')),
 CHECK(observation_json->>'observationState'<>'UNVERSIONED_READ' OR(risk='READ' AND approval_policy='NONE')),
 CHECK((node_pin_ref IS NULL)=(node_pin_generation IS NULL)),
 CHECK(input_digest=encode(public.digest(convert_to(input_json,'UTF8'),'sha256'),'hex'))
);
-- Один closed predicate для child grant применяется к idle readback и frame admission.
-- Root grant не расширяет namespace/capability дочернего effect; latest RR должен
-- содержать точный child grant/version, а другая recipient identity не допускается.
-- +goose StatementBegin
CREATE FUNCTION control_plane.kubernetes_execution_child_grant_current(child_id uuid,invocation_id uuid) RETURNS boolean
LANGUAGE sql STABLE SET search_path=pg_catalog AS $child_current$
 SELECT EXISTS(
  SELECT 1 FROM control_plane.kubernetes_execution_children child
  JOIN control_plane.kubernetes_execution_plans plan ON plan.id=child.plan_id AND plan.organization_id=child.organization_id
  JOIN control_plane.integration_invocations invocation ON invocation.id=plan.invocation_id AND invocation.organization_id=plan.organization_id
  JOIN control_plane.integration_grants parent_grant ON parent_grant.id=invocation.grant_id AND parent_grant.organization_id=invocation.organization_id
  JOIN control_plane.integration_grants grant_binding ON grant_binding.id=child.grant_id AND grant_binding.organization_id=child.organization_id
  JOIN control_plane.kubernetes_invocation_identity_bindings pin ON pin.invocation_id=invocation.id AND pin.organization_id=invocation.organization_id
  WHERE child.id=$1 AND invocation.id=$2 AND invocation.resource_kind='KUBERNETES_TARGET'
   AND grant_binding.enabled AND grant_binding.version=child.grant_version
   AND grant_binding.connection_id=invocation.connection_id AND grant_binding.capability_key=child.capability_key
   AND grant_binding.resource_kind='KUBERNETES_TARGET' AND grant_binding.resource_scope_digest=child.grant_scope_digest
   AND grant_binding.definition_version=invocation.definition_version AND grant_binding.definition_digest=invocation.definition_digest
   AND grant_binding.approval_policy=child.approval_policy AND grant_binding.risk=child.risk
   AND grant_binding.target_kind=parent_grant.target_kind AND grant_binding.target_ref=parent_grant.target_ref
   AND grant_binding.resource_scope->>'target_ref'=pin.target_ref
   AND grant_binding.resource_scope->>'namespace'=CASE
    WHEN coalesce((child.observation_json->'resource'->>'allNamespaces')::boolean,false) THEN '@all-namespaces'
    WHEN coalesce((child.observation_json->'resource'->>'namespaced')::boolean,false) THEN child.observation_json->'resource'->>'namespace'
    ELSE '@cluster' END
   AND EXISTS(SELECT 1 FROM control_plane.runtime_revisions revision,
    jsonb_array_elements(coalesce(revision.safe_snapshot->'integrationGrants','[]'::jsonb)) binding
    WHERE revision.node_id=invocation.node_id AND revision.organization_id=invocation.organization_id
     AND revision.generation=(SELECT max(latest.generation) FROM control_plane.runtime_revisions latest WHERE latest.node_id=invocation.node_id)
     AND binding->>'ref'=grant_binding.ref AND binding->>'capabilityKey'=grant_binding.capability_key
     AND binding->>'grantVersion'=grant_binding.version::text AND binding->>'approvalPolicy'=grant_binding.approval_policy)
 )
$child_current$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.kubernetes_execution_child_grant_current(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.kubernetes_execution_child_grant_current(uuid,uuid) TO control_plane_runtime;
CREATE TABLE control_plane.kubernetes_execution_effects (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 ref text NOT NULL UNIQUE CHECK(ref ~ '^kef_[A-Za-z0-9_-]{8,89}$'),
 organization_id uuid NOT NULL REFERENCES control_plane.organizations(id) DEFERRABLE INITIALLY IMMEDIATE,
 child_id uuid NOT NULL UNIQUE,
 generation bigint NOT NULL CHECK(generation=1),
 state text NOT NULL CHECK(state IN('ATTEMPTING','SUCCEEDED','FAILED','UNKNOWN','CANCELLED')),
 admission_expires_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL,
 closed_at timestamptz,
 UNIQUE(id,organization_id),
 FOREIGN KEY(child_id,organization_id) REFERENCES control_plane.kubernetes_execution_children(id,organization_id) DEFERRABLE INITIALLY IMMEDIATE,
 CHECK(admission_expires_at>created_at AND admission_expires_at<=created_at+interval '30 seconds'),
 CHECK((state='ATTEMPTING')=(closed_at IS NULL))
);
CREATE TABLE control_plane.kubernetes_execution_streams (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 ref text NOT NULL UNIQUE CHECK(ref ~ '^kst_[A-Za-z0-9_-]{8,89}$'),
 organization_id uuid NOT NULL REFERENCES control_plane.organizations(id) DEFERRABLE INITIALLY IMMEDIATE,
 effect_id uuid NOT NULL UNIQUE,
 generation bigint NOT NULL CHECK(generation>0),
 state text NOT NULL CHECK(state IN('PREPARED','ACTIVE','COMPLETED','CANCELED','REVOKED','EXPIRED','LOST')),
 hard_deadline timestamptz NOT NULL,
 maximum_frame_bytes integer NOT NULL CHECK(maximum_frame_bytes BETWEEN 1 AND 32768),
 maximum_total_bytes bigint NOT NULL CHECK(maximum_total_bytes BETWEEN 1 AND 268435456),
 input_channels text[] NOT NULL CHECK(cardinality(input_channels)<=7),
 output_channels text[] NOT NULL CHECK(cardinality(output_channels) BETWEEN 1 AND 7),
 input_version bigint NOT NULL DEFAULT 1 CHECK(input_version>0),
 input_sequence bigint NOT NULL DEFAULT 0 CHECK(input_sequence>=0),
 output_sequence bigint NOT NULL DEFAULT 0 CHECK(output_sequence>=0),
 logical_bytes bigint NOT NULL DEFAULT 0 CHECK(logical_bytes>=0),
 delivered_bytes bigint NOT NULL DEFAULT 0 CHECK(delivered_bytes>=logical_bytes),
 closed_input_channels text[] NOT NULL DEFAULT '{}' CHECK(cardinality(closed_input_channels)<=7 AND closed_input_channels<@input_channels),
 stdin_closed boolean NOT NULL DEFAULT false,
 handshake_digest text CHECK(handshake_digest ~ '^[a-f0-9]{64}$'),
 handshake_json jsonb CHECK(jsonb_typeof(handshake_json)='object' AND octet_length(handshake_json::text)<=8192),
 terminal_digest text CHECK(terminal_digest ~ '^[a-f0-9]{64}$'),
 terminal_json jsonb CHECK(jsonb_typeof(terminal_json)='object' AND octet_length(terminal_json::text)<=8192),
 remote_outcome text NOT NULL DEFAULT 'UNKNOWN' CHECK(remote_outcome IN('SUCCEEDED','FAILED','UNKNOWN')),
 created_at timestamptz NOT NULL,
 closed_at timestamptz,
 UNIQUE(id,organization_id),
 FOREIGN KEY(effect_id,organization_id) REFERENCES control_plane.kubernetes_execution_effects(id,organization_id) DEFERRABLE INITIALLY IMMEDIATE,
 CHECK(hard_deadline>created_at AND hard_deadline<=created_at+interval '600 seconds'),
 CHECK(delivered_bytes<=maximum_total_bytes),
 CHECK((handshake_digest IS NULL)=(handshake_json IS NULL)),
 CHECK((terminal_digest IS NULL)=(terminal_json IS NULL)),
 CHECK((state IN('PREPARED','ACTIVE'))=(closed_at IS NULL))
);
CREATE TABLE control_plane.kubernetes_execution_frames (
 stream_id uuid NOT NULL,
 organization_id uuid NOT NULL REFERENCES control_plane.organizations(id) DEFERRABLE INITIALLY IMMEDIATE,
 direction text NOT NULL CHECK(direction IN('INPUT','OUTPUT')),
 sequence bigint NOT NULL CHECK(sequence>0),
 channel text NOT NULL CHECK(channel IN('CONTROL','STDIN','STDOUT','STDERR','TERMINAL_RESIZE','PORT_FORWARD','FILE_CONTENT')),
 payload_digest text NOT NULL CHECK(payload_digest ~ '^[a-f0-9]{64}$'),
 byte_count integer NOT NULL CHECK(byte_count BETWEEN 0 AND 32768),
 end_of_input boolean NOT NULL,
 delivery_credits integer NOT NULL CHECK(delivery_credits BETWEEN 1 AND 4 AND(direction='OUTPUT' OR delivery_credits=1)),
 grant_expires_at timestamptz NOT NULL,
 acknowledged_at timestamptz,
 created_at timestamptz NOT NULL,
 PRIMARY KEY(stream_id,direction,sequence),
 FOREIGN KEY(stream_id,organization_id) REFERENCES control_plane.kubernetes_execution_streams(id,organization_id) DEFERRABLE INITIALLY IMMEDIATE,
 CHECK(byte_count>0 OR end_of_input)
);
-- Private INPUT stages принадлежат exact stream, actor и immutable sequence; bytes хранятся лишь в versioned object storage.
CREATE TABLE control_plane.kubernetes_input_stages (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 ref text NOT NULL UNIQUE CHECK(ref ~ '^kif_[A-Za-z0-9_-]{8,89}$'),
 organization_id uuid NOT NULL REFERENCES control_plane.organizations(id) DEFERRABLE INITIALLY IMMEDIATE,
 stream_id uuid NOT NULL,
 actor_id uuid NOT NULL REFERENCES control_plane.subjects(id) DEFERRABLE INITIALLY IMMEDIATE,
 submission_key text NOT NULL CHECK(octet_length(submission_key) BETWEEN 8 AND 128),
 intent_digest text NOT NULL CHECK(intent_digest ~ '^[a-f0-9]{64}$'),
 sequence bigint NOT NULL CHECK(sequence>0),
 channel text NOT NULL CHECK(channel IN('STDIN','TERMINAL_RESIZE','PORT_FORWARD','FILE_CONTENT')),
 payload_digest text NOT NULL CHECK(payload_digest ~ '^[a-f0-9]{64}$'),
 byte_count integer NOT NULL CHECK(byte_count BETWEEN 0 AND 32768),
 end_of_input boolean NOT NULL,
 state text NOT NULL CHECK(state IN('ATTEMPTING','QUEUED','CLAIMED','ACKNOWLEDGED','UNKNOWN','REJECTED','CANCELLED')),
 -- FILE stages содержат immutable range metadata; отдельный object принадлежит только обычному INPUT.
 object_key text,
 CONSTRAINT kubernetes_input_file_metadata_no_object CHECK(CASE WHEN channel='FILE_CONTENT' THEN object_key IS NULL AND object_version IS NULL AND object_etag IS NULL
  ELSE object_key IS NOT NULL AND object_key='organizations/'||organization_id::text||'/kubernetes-input-stages/'||ref END),
 object_version text CHECK(octet_length(object_version) BETWEEN 1 AND 1024 AND lower(object_version)<>'null'),
 object_etag text CHECK(octet_length(object_etag) BETWEEN 1 AND 1024),
 delivery_expires_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 recorded_at timestamptz,
 claimed_at timestamptz,
 native_admitted_at timestamptz,
 acknowledged_at timestamptz,
 UNIQUE(stream_id,sequence),UNIQUE(organization_id,stream_id,actor_id,submission_key),
 FOREIGN KEY(stream_id,organization_id) REFERENCES control_plane.kubernetes_execution_streams(id,organization_id) DEFERRABLE INITIALLY IMMEDIATE,
 CHECK(byte_count>0 OR end_of_input),
 CHECK((object_version IS NULL)=(object_etag IS NULL)),
 CHECK(channel='FILE_CONTENT' OR state NOT IN('QUEUED','CLAIMED','ACKNOWLEDGED') OR object_version IS NOT NULL),
 CHECK((claimed_at IS NULL)=(delivery_expires_at IS NULL)),
 CHECK(native_admitted_at IS NULL OR claimed_at IS NOT NULL),
 CHECK((state='ACKNOWLEDGED')=(acknowledged_at IS NOT NULL)),
 CHECK(acknowledged_at IS NULL OR native_admitted_at IS NOT NULL)
);
CREATE INDEX kubernetes_input_stage_org ON control_plane.kubernetes_input_stages(organization_id);
-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_kubernetes_input_stage() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.state<>'ATTEMPTING' OR NEW.object_version IS NOT NULL OR NEW.claimed_at IS NOT NULL OR NEW.native_admitted_at IS NOT NULL OR NEW.recorded_at IS NOT NULL
   OR NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_execution_streams stream
    JOIN control_plane.kubernetes_execution_effects effect ON effect.id=stream.effect_id AND effect.organization_id=stream.organization_id
    JOIN control_plane.kubernetes_execution_children child ON child.id=effect.child_id AND child.organization_id=effect.organization_id
    JOIN control_plane.kubernetes_execution_plans plan ON plan.id=child.plan_id AND plan.organization_id=child.organization_id
    JOIN control_plane.kubernetes_invocation_identity_bindings pin ON pin.invocation_id=plan.invocation_id AND pin.organization_id=plan.organization_id
    WHERE stream.id=NEW.stream_id AND stream.organization_id=NEW.organization_id AND stream.state='ACTIVE'
     AND pin.root_actor_id=NEW.actor_id AND NEW.sequence=stream.input_sequence+1 AND NEW.channel=ANY(stream.input_channels)
     AND NEW.byte_count<=stream.maximum_frame_bytes
     AND NOT NEW.channel=ANY(stream.closed_input_channels) AND stream.hard_deadline>clock_timestamp()) THEN
   RAISE EXCEPTION 'invalid Kubernetes input reservation' USING ERRCODE='23514';
  END IF;
 ELSE
  IF ROW(NEW.id,NEW.ref,NEW.organization_id,NEW.stream_id,NEW.actor_id,NEW.submission_key,NEW.intent_digest,NEW.sequence,NEW.channel,NEW.payload_digest,NEW.byte_count,NEW.end_of_input,NEW.object_key,NEW.created_at)
   IS DISTINCT FROM ROW(OLD.id,OLD.ref,OLD.organization_id,OLD.stream_id,OLD.actor_id,OLD.submission_key,OLD.intent_digest,OLD.sequence,OLD.channel,OLD.payload_digest,OLD.byte_count,OLD.end_of_input,OLD.object_key,OLD.created_at)
   OR OLD.recorded_at IS NOT NULL AND ROW(NEW.object_version,NEW.object_etag,NEW.recorded_at) IS DISTINCT FROM ROW(OLD.object_version,OLD.object_etag,OLD.recorded_at)
   OR OLD.claimed_at IS NOT NULL AND ROW(NEW.claimed_at,NEW.delivery_expires_at) IS DISTINCT FROM ROW(OLD.claimed_at,OLD.delivery_expires_at)
   OR OLD.native_admitted_at IS NOT NULL AND NEW.native_admitted_at IS DISTINCT FROM OLD.native_admitted_at
   OR OLD.acknowledged_at IS NOT NULL AND NEW.acknowledged_at IS DISTINCT FROM OLD.acknowledged_at
   OR NOT(NEW.state=OLD.state OR OLD.state='ATTEMPTING' AND NEW.state IN('QUEUED','UNKNOWN','REJECTED','CANCELLED')
    OR OLD.state='QUEUED' AND NEW.state IN('CLAIMED','CANCELLED')
    OR OLD.state='CLAIMED' AND NEW.state IN('UNKNOWN','CANCELLED','ACKNOWLEDGED')
    OR OLD.state IN('UNKNOWN','CANCELLED') AND NEW.state='ACKNOWLEDGED' AND OLD.native_admitted_at IS NOT NULL) THEN
   RAISE EXCEPTION 'immutable Kubernetes input fact' USING ERRCODE='23514';
  END IF;
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_kubernetes_input_stage() FROM PUBLIC;
CREATE TRIGGER kubernetes_input_stage_guard BEFORE INSERT OR UPDATE ON control_plane.kubernetes_input_stages
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_kubernetes_input_stage();
-- +goose StatementBegin
CREATE FUNCTION control_plane.advance_kubernetes_input_version() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF TG_OP='UPDATE' AND ROW(NEW.state,NEW.recorded_at,NEW.claimed_at,NEW.delivery_expires_at,NEW.native_admitted_at,NEW.acknowledged_at)
  IS NOT DISTINCT FROM ROW(OLD.state,OLD.recorded_at,OLD.claimed_at,OLD.delivery_expires_at,OLD.native_admitted_at,OLD.acknowledged_at) THEN RETURN NULL; END IF;
 UPDATE control_plane.kubernetes_execution_streams SET input_version=input_version+1
 WHERE id=NEW.stream_id AND organization_id=NEW.organization_id;
 RETURN NULL;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.advance_kubernetes_input_version() FROM PUBLIC;
CREATE TRIGGER kubernetes_input_stage_version AFTER INSERT OR UPDATE ON control_plane.kubernetes_input_stages
 FOR EACH ROW EXECUTE FUNCTION control_plane.advance_kubernetes_input_version();
-- +goose StatementBegin
CREATE FUNCTION control_plane.close_kubernetes_input_stages() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF NEW.state NOT IN('PREPARED','ACTIVE') AND NEW.state IS DISTINCT FROM OLD.state THEN
  UPDATE control_plane.kubernetes_execution_streams SET input_version=input_version+1
  WHERE id=NEW.id AND organization_id=NEW.organization_id;
  UPDATE control_plane.kubernetes_input_stages SET state=CASE WHEN state='CLAIMED' THEN 'UNKNOWN' ELSE 'CANCELLED' END
  WHERE stream_id=NEW.id AND organization_id=NEW.organization_id AND state IN('ATTEMPTING','QUEUED','CLAIMED');
 END IF;
 RETURN NULL;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.close_kubernetes_input_stages() FROM PUBLIC;
CREATE TRIGGER kubernetes_input_stream_closure AFTER UPDATE OF state ON control_plane.kubernetes_execution_streams
 FOR EACH ROW EXECUTE FUNCTION control_plane.close_kubernetes_input_stages();
REVOKE ALL ON control_plane.kubernetes_input_stages FROM control_plane_runtime;
GRANT SELECT,INSERT,UPDATE ON control_plane.kubernetes_input_stages TO control_plane_runtime;
-- Private OUTPUT bytes имеют собственный one-write intent; frame ledger остаётся владельцем credit.
-- Ни object key, ни версия, ни bytes не попадают в invocation JSON, public RPC либо событие.
CREATE TABLE control_plane.kubernetes_output_stages (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 ref text NOT NULL UNIQUE CHECK(ref ~ '^kof_[A-Za-z0-9_-]{8,89}$'),
 organization_id uuid NOT NULL REFERENCES control_plane.organizations(id) DEFERRABLE INITIALLY IMMEDIATE,
 stream_id uuid NOT NULL,
 direction text NOT NULL DEFAULT 'OUTPUT' CHECK(direction='OUTPUT'),
 sequence bigint NOT NULL CHECK(sequence>0),
 channel text NOT NULL CHECK(channel IN('CONTROL','STDOUT','STDERR','PORT_FORWARD','FILE_CONTENT')),
 payload_digest text NOT NULL CHECK(payload_digest ~ '^[a-f0-9]{64}$'),
 byte_count integer NOT NULL CHECK(byte_count BETWEEN 1 AND 32768),
 delivery_ordinal integer NOT NULL CHECK(delivery_ordinal BETWEEN 1 AND 4),
 state text NOT NULL CHECK(state IN('ATTEMPTING','STAGED','ACCEPTED','UNKNOWN','REJECTED')),
 authorized_until timestamptz NOT NULL,
 object_key text NOT NULL CHECK(object_key='organizations/'||organization_id::text||'/kubernetes-output-stages/'||ref),
 object_version text CHECK(octet_length(object_version) BETWEEN 1 AND 1024 AND lower(object_version)<>'null'),
 object_etag text CHECK(octet_length(object_etag) BETWEEN 1 AND 1024),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 recorded_at timestamptz,
 accepted_at timestamptz,
 UNIQUE(stream_id,sequence),
 FOREIGN KEY(stream_id,organization_id) REFERENCES control_plane.kubernetes_execution_streams(id,organization_id) DEFERRABLE INITIALLY IMMEDIATE,
 FOREIGN KEY(stream_id,direction,sequence) REFERENCES control_plane.kubernetes_execution_frames(stream_id,direction,sequence) DEFERRABLE INITIALLY IMMEDIATE,
 CHECK((state='ATTEMPTING')=(recorded_at IS NULL)),
 CHECK((state IN('STAGED','ACCEPTED'))=(object_version IS NOT NULL AND object_etag IS NOT NULL)),
 CHECK((state='ACCEPTED')=(accepted_at IS NOT NULL))
);
CREATE INDEX kubernetes_output_stages_organization ON control_plane.kubernetes_output_stages(organization_id);
-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_kubernetes_output_stage() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 -- Очистка меняет только собственные поля под отдельным owner fence. Она не создаёт
 -- frame/ACK/credit и не требует живого native grant после Stop. Отдельный cleanup
 -- trigger и SECURITY DEFINER claim/complete проверяют полномочия и текущую попытку.
 IF TG_OP='UPDATE' AND current_user='control_plane_owner'
  AND to_jsonb(NEW)-ARRAY['cleanup_generation','cleanup_owner','cleanup_deadline','cleanup_attempts','cleanup_total_attempts','cleanup_repair_generation',
   'cleanup_object_version','cleanup_object_etag','payload_purged_at','deletion_receipt_digest']
   =to_jsonb(OLD)-ARRAY['cleanup_generation','cleanup_owner','cleanup_deadline','cleanup_attempts','cleanup_total_attempts','cleanup_repair_generation',
   'cleanup_object_version','cleanup_object_etag','payload_purged_at','deletion_receipt_digest'] THEN
  RETURN NEW;
 END IF;

 IF TG_OP='INSERT' THEN
  IF NEW.state<>'ATTEMPTING' OR NEW.recorded_at IS NOT NULL OR NEW.accepted_at IS NOT NULL THEN
   RAISE EXCEPTION 'invalid Kubernetes output reservation' USING ERRCODE='23514';
  END IF;
 ELSE
  IF ROW(NEW.id,NEW.ref,NEW.organization_id,NEW.stream_id,NEW.direction,NEW.sequence,NEW.channel,NEW.payload_digest,NEW.byte_count,NEW.object_key,NEW.created_at)
    IS DISTINCT FROM ROW(OLD.id,OLD.ref,OLD.organization_id,OLD.stream_id,OLD.direction,OLD.sequence,OLD.channel,OLD.payload_digest,OLD.byte_count,OLD.object_key,OLD.created_at)
   OR OLD.recorded_at IS NOT NULL AND ROW(NEW.object_version,NEW.object_etag,NEW.recorded_at) IS DISTINCT FROM ROW(OLD.object_version,OLD.object_etag,OLD.recorded_at)
   OR OLD.accepted_at IS NOT NULL AND NEW.accepted_at IS DISTINCT FROM OLD.accepted_at
   OR NOT(OLD.state='ATTEMPTING' AND NEW.state IN('STAGED','UNKNOWN','REJECTED')
       OR OLD.state IN('STAGED','ACCEPTED') AND NEW.state='ACCEPTED')
   OR NEW.delivery_ordinal<OLD.delivery_ordinal
   OR NEW.delivery_ordinal=OLD.delivery_ordinal AND NEW.authorized_until IS DISTINCT FROM OLD.authorized_until THEN
   RAISE EXCEPTION 'immutable Kubernetes output fact' USING ERRCODE='23514';
  END IF;
 END IF;
 IF TG_OP='INSERT' OR NEW.state='ACCEPTED' THEN
  IF NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_execution_frames frame
   JOIN control_plane.kubernetes_execution_streams stream ON stream.id=frame.stream_id AND stream.organization_id=frame.organization_id
   WHERE frame.stream_id=NEW.stream_id AND frame.organization_id=NEW.organization_id AND frame.direction='OUTPUT'
    AND frame.sequence=NEW.sequence AND frame.channel=NEW.channel AND frame.payload_digest=NEW.payload_digest
    AND frame.byte_count=NEW.byte_count AND frame.delivery_credits=NEW.delivery_ordinal
    AND frame.grant_expires_at=NEW.authorized_until AND frame.grant_expires_at>clock_timestamp()
    AND frame.acknowledged_at IS NULL AND stream.state='ACTIVE') THEN
   RAISE EXCEPTION 'Kubernetes output credit is unavailable' USING ERRCODE='23514';
  END IF;
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_kubernetes_output_stage() FROM PUBLIC;
CREATE TRIGGER kubernetes_output_stage_guard BEFORE INSERT OR UPDATE ON control_plane.kubernetes_output_stages
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_kubernetes_output_stage();
REVOKE ALL ON control_plane.kubernetes_output_stages FROM control_plane_runtime;
GRANT SELECT,INSERT,UPDATE ON control_plane.kubernetes_output_stages TO control_plane_runtime;
-- Capture loss facts не создают frame/ACK и не входят в successful delivery counters.
-- Native ordinal различает одинаковые DATA; повтор одного события сохраняет исходный tuple.
CREATE TABLE control_plane.kubernetes_capture_losses (
 organization_id uuid NOT NULL,
 stream_id uuid NOT NULL,
 source_ref text NOT NULL CHECK(source_ref ~ '^kpf_[A-Za-z0-9_-]{8,89}$'),
 source_generation bigint NOT NULL CHECK(source_generation>0),
 native_ordinal bigint NOT NULL CHECK(native_ordinal>0),
 payload_digest text NOT NULL CHECK(payload_digest ~ '^[a-f0-9]{64}$'),
 byte_count integer NOT NULL CHECK(byte_count BETWEEN 1 AND 32768),
 reason_code text NOT NULL CHECK(reason_code='PORT_FORWARD_CONNECTION_CLOSED'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(organization_id,stream_id,source_ref,source_generation,native_ordinal),
 FOREIGN KEY(stream_id,organization_id) REFERENCES control_plane.kubernetes_execution_streams(id,organization_id) DEFERRABLE INITIALLY IMMEDIATE
);
-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_kubernetes_capture_loss() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
DECLARE budget bigint; used bigint; logical bigint; frame_limit integer;
BEGIN
 SELECT stream.maximum_total_bytes,stream.logical_bytes,stream.maximum_frame_bytes INTO budget,logical,frame_limit FROM control_plane.kubernetes_execution_streams stream
 JOIN control_plane.kubernetes_execution_effects effect ON effect.id=stream.effect_id AND effect.organization_id=stream.organization_id
 JOIN control_plane.kubernetes_execution_children child ON child.id=effect.child_id AND child.organization_id=effect.organization_id
 WHERE stream.id=NEW.stream_id AND stream.organization_id=NEW.organization_id AND stream.state='ACTIVE'
  AND child.operation='kubernetes.port-forward' FOR UPDATE OF stream;
 IF NOT FOUND THEN RAISE EXCEPTION 'Kubernetes capture source is unavailable' USING ERRCODE='23514'; END IF;
 SELECT COALESCE(sum(byte_count),0) INTO used FROM control_plane.kubernetes_capture_losses
 WHERE stream_id=NEW.stream_id AND organization_id=NEW.organization_id;
 IF NEW.byte_count>frame_limit OR logical>budget OR used>budget-logical OR NEW.byte_count>budget-logical-used THEN
  RAISE EXCEPTION 'Kubernetes capture loss budget is unavailable' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_kubernetes_capture_loss() FROM PUBLIC;
CREATE TRIGGER kubernetes_capture_loss_guard BEFORE INSERT ON control_plane.kubernetes_capture_losses
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_kubernetes_capture_loss();
CREATE TRIGGER kubernetes_capture_loss_immutable BEFORE UPDATE OR DELETE ON control_plane.kubernetes_capture_losses
 FOR EACH ROW WHEN(NOT control_plane.project_purge_authorized()) EXECUTE FUNCTION control_plane.reject_integration_immutable_update();
REVOKE ALL ON control_plane.kubernetes_capture_losses FROM control_plane_runtime;
GRANT SELECT,INSERT ON control_plane.kubernetes_capture_losses TO control_plane_runtime;
-- Capture generations фиксируют owner-ACK prefix после owner closure. FULL требует
-- typed positive join; PARTIAL не утверждает stop, join либо весь native transcript.
ALTER TABLE control_plane.kubernetes_execution_streams ADD COLUMN capture_generation bigint NOT NULL DEFAULT 0 CHECK(capture_generation>=0);
-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_kubernetes_capture_generation() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF NEW.capture_generation IS DISTINCT FROM OLD.capture_generation AND
  (current_user<>'control_plane_owner' OR OLD.capture_generation=9223372036854775807
   OR NEW.capture_generation<>OLD.capture_generation+1 OR NEW.state IN('PREPARED','ACTIVE')) THEN
  RAISE EXCEPTION 'Kubernetes capture generation requires owner seal' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_kubernetes_capture_generation() FROM PUBLIC;
CREATE TRIGGER kubernetes_capture_generation_owner BEFORE UPDATE OF capture_generation ON control_plane.kubernetes_execution_streams
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_kubernetes_capture_generation();

CREATE TABLE control_plane.kubernetes_output_captures (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 ref text NOT NULL UNIQUE CHECK(ref ~ '^koc_[A-Za-z0-9_-]{8,89}$'),
 organization_id uuid NOT NULL REFERENCES control_plane.organizations(id) DEFERRABLE INITIALLY IMMEDIATE,
 stream_id uuid NOT NULL,
 capture_generation bigint NOT NULL CHECK(capture_generation>0),
 stream_generation bigint NOT NULL CHECK(stream_generation>0),
 terminal_digest text NOT NULL CHECK(terminal_digest='' OR terminal_digest ~ '^[a-f0-9]{64}$'),
 source_snapshot text NOT NULL CHECK(octet_length(source_snapshot)<=16384 AND jsonb_typeof(source_snapshot::jsonb)='object'),
 snapshot_digest text NOT NULL CHECK(snapshot_digest=encode(public.digest(convert_to(source_snapshot,'UTF8'),'sha256'),'hex')),
 output_watermark bigint NOT NULL CHECK(output_watermark>=0),
 acknowledged_watermark bigint NOT NULL CHECK(acknowledged_watermark>=0 AND acknowledged_watermark<=output_watermark),
 sealed_at timestamptz NOT NULL,
 retain_until timestamptz,
 state text NOT NULL DEFAULT 'READY' CHECK(state IN('READY','CLAIMED','COMPLETE','UNKNOWN','REJECTED','WAITING_OWNER','SUPERSEDED')),
 capture_kind text CHECK(capture_kind IN('EMPTY','FULL','PARTIAL')),
 source_digest text CHECK(source_digest ~ '^[a-f0-9]{64}$'),
 acknowledged_prefix bigint CHECK(acknowledged_prefix>=0 AND acknowledged_prefix<=acknowledged_watermark),
 dropped_bytes bigint CHECK(dropped_bytes>=0),
 generation bigint NOT NULL DEFAULT 0 CHECK(generation>=0),
 attempt_count bigint NOT NULL DEFAULT 0 CHECK(attempt_count>=0),
 version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 repair_generation bigint NOT NULL DEFAULT 0 CHECK(repair_generation>=0),
 repair_base_attempt_count bigint NOT NULL DEFAULT 0 CHECK(repair_base_attempt_count BETWEEN 0 AND attempt_count),
 next_attempt_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 reason_code text NOT NULL DEFAULT '' CHECK(reason_code IN('','CAPTURE_ELIGIBILITY_CHANGED','CAPTURE_SOURCE_INVALID','CAPTURE_STORAGE_UNKNOWN','CAPTURE_RETRY_EXHAUSTED','CAPTURE_RETENTION_EXPIRED','CAPTURE_CONTENT_REJECTED','CAPTURE_SUPERSEDED')),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(id,organization_id),UNIQUE(stream_id,capture_generation),
 FOREIGN KEY(stream_id,organization_id) REFERENCES control_plane.kubernetes_execution_streams(id,organization_id) DEFERRABLE INITIALLY IMMEDIATE,
 CHECK(retain_until IS NULL OR retain_until>sealed_at),
 CHECK((state='COMPLETE')=(capture_kind IS NOT NULL AND source_digest IS NOT NULL AND acknowledged_prefix IS NOT NULL AND dropped_bytes IS NOT NULL))
);
CREATE INDEX kubernetes_output_capture_due ON control_plane.kubernetes_output_captures(next_attempt_at,id) WHERE state IN('READY','CLAIMED');
CREATE TABLE control_plane.kubernetes_capture_attempts (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 ref text NOT NULL UNIQUE CHECK(ref ~ '^kca_[A-Za-z0-9_-]{8,89}$'),
 organization_id uuid NOT NULL,
 capture_id uuid NOT NULL,
 generation bigint NOT NULL CHECK(generation>0),
 workload_instance text NOT NULL CHECK(octet_length(workload_instance) BETWEEN 1 AND 128),
 fence_digest text NOT NULL CHECK(fence_digest ~ '^[a-f0-9]{64}$'),
 expires_at timestamptz NOT NULL,
 hard_deadline timestamptz NOT NULL,
 state text NOT NULL CHECK(state IN('ACTIVE','EXPIRED','COMPLETE','UNKNOWN','CANCELLED')),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 completed_at timestamptz,
 UNIQUE(capture_id,generation),UNIQUE(id,organization_id),
 FOREIGN KEY(capture_id,organization_id) REFERENCES control_plane.kubernetes_output_captures(id,organization_id) DEFERRABLE INITIALLY IMMEDIATE,
 CHECK(expires_at>created_at AND expires_at<=hard_deadline),CHECK((state='ACTIVE')=(completed_at IS NULL))
);
CREATE UNIQUE INDEX kubernetes_capture_one_active_attempt ON control_plane.kubernetes_capture_attempts(capture_id) WHERE state='ACTIVE';
-- +goose StatementBegin
CREATE FUNCTION control_plane.kubernetes_capture_source_snapshot(p_stream uuid,p_org uuid,p_sealed timestamptz,p_generation bigint,p_previous text)
RETURNS jsonb LANGUAGE sql VOLATILE SET search_path=pg_catalog AS $$
 SELECT jsonb_build_object(
  'version',1,'organizationId',organization.id::text,'organizationRef',organization.ref,
  'rootActorId',actor.id::text,'rootActorRef',actor.ref,'projectId',coalesce(project.id::text,''),'projectRef',coalesce(project.ref,''),
  'runId',run.id::text,'runRef',run.ref,'rootRunId',root.id::text,'rootRunRef',root.ref,
  'nodeId',node.id::text,'nodeRef',node.ref,'invocationRef',invocation.ref,'streamRef',stream.ref,'generation',stream.generation,
  'operation',child.operation,'credentialPolicy',pin.credential_policy,'subjectId',subject.id::text,'subjectRef',subject.ref,'subjectKind',subject.kind,
  'identityRef',identity.ref,'identityRevision',identity.revision,'bindingDigest',plan.binding_digest,'snapshotDigest',plan.snapshot_digest,
  'childInputDigest',child.input_digest,'planRef',plan.ref,'planDigest',plan.plan_digest,'terminalDigest',coalesce(stream.terminal_digest,''),
  'targetRef',pin.target_ref,'targetRevision',pin.target_revision,'originalLeaseRef',invocation.claimed_lease_ref,
  'originalFenceDigest',invocation.claimed_fence_digest,'outputWatermark',stream.output_sequence,'maximumTotalBytes',stream.maximum_total_bytes,
  'sealedAt',p_sealed,'captureGeneration',p_generation,'previousCaptureRef',p_previous,
  'acknowledgedWatermark',coalesce((SELECT max(frame.sequence) FROM control_plane.kubernetes_execution_frames frame
   WHERE frame.stream_id=stream.id AND frame.organization_id=stream.organization_id AND frame.direction='OUTPUT' AND frame.acknowledged_at<=p_sealed),0),
  'ownerState',stream.state,'nativeTerminalKnown',stream.terminal_digest IS NOT NULL,
  'transportJoined',coalesce(stream.terminal_json->'transportJoined'='true'::jsonb,false))
  ||jsonb_build_object('connectionRef',connection.ref,'resource',child.observation_json->'resource',
   'effectRef',effect.ref,'effectGeneration',effect.generation,'terminalReceipt',stream.terminal_json,
   'lossBytes',coalesce((SELECT sum(loss.byte_count) FROM control_plane.kubernetes_capture_losses loss WHERE loss.stream_id=stream.id AND loss.organization_id=stream.organization_id AND loss.created_at<=p_sealed),0),
   'lossFacts',(SELECT count(*) FROM control_plane.kubernetes_capture_losses loss WHERE loss.stream_id=stream.id AND loss.organization_id=stream.organization_id AND loss.created_at<=p_sealed))
 FROM control_plane.kubernetes_execution_streams stream
 JOIN control_plane.organizations organization ON organization.id=stream.organization_id
 JOIN control_plane.kubernetes_execution_effects effect ON effect.id=stream.effect_id AND effect.organization_id=stream.organization_id
 JOIN control_plane.kubernetes_execution_children child ON child.id=effect.child_id AND child.organization_id=effect.organization_id
 JOIN control_plane.kubernetes_execution_plans plan ON plan.id=child.plan_id AND plan.organization_id=child.organization_id
 JOIN control_plane.integration_invocations invocation ON invocation.id=plan.invocation_id AND invocation.organization_id=plan.organization_id
 JOIN control_plane.integration_connections connection ON connection.id=invocation.connection_id AND connection.organization_id=invocation.organization_id
 JOIN control_plane.kubernetes_invocation_identity_bindings pin ON pin.invocation_id=invocation.id AND pin.organization_id=invocation.organization_id
 JOIN control_plane.subjects actor ON actor.id=pin.root_actor_id AND actor.organization_id=pin.organization_id
 JOIN control_plane.subjects subject ON subject.id=pin.subject_id AND subject.organization_id=pin.organization_id
 JOIN control_plane.integration_broker_identity_revisions identity ON identity.id=pin.identity_revision_id AND identity.organization_id=pin.organization_id
 JOIN control_plane.runs run ON run.id=invocation.run_id AND run.organization_id=invocation.organization_id
 JOIN control_plane.runs root ON root.id=pin.root_run_id AND root.organization_id=pin.organization_id AND root.initiated_by=actor.id
 JOIN control_plane.run_nodes node ON node.id=invocation.node_id AND node.organization_id=invocation.organization_id
 LEFT JOIN control_plane.projects project ON project.id=root.project_id AND project.organization_id=pin.organization_id
 WHERE stream.id=p_stream AND stream.organization_id=p_org AND stream.state NOT IN('PREPARED','ACTIVE');
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.kubernetes_capture_source_snapshot(uuid,uuid,timestamptz,bigint,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.kubernetes_capture_source_snapshot(uuid,uuid,timestamptz,bigint,text) TO control_plane_runtime;
-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_kubernetes_output_capture() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
DECLARE expected jsonb; previous_ref text;
BEGIN
 IF TG_OP='INSERT' THEN
  SELECT ref INTO previous_ref FROM control_plane.kubernetes_output_captures
   WHERE stream_id=NEW.stream_id AND organization_id=NEW.organization_id AND capture_generation<NEW.capture_generation
   ORDER BY capture_generation DESC LIMIT 1;
  expected:=control_plane.kubernetes_capture_source_snapshot(NEW.stream_id,NEW.organization_id,NEW.sealed_at,NEW.capture_generation,coalesce(previous_ref,''));
  IF NEW.state<>'READY' OR NEW.generation<>0 OR NEW.attempt_count<>0 OR NEW.version<>1
   OR NEW.repair_generation<>0 OR NEW.repair_base_attempt_count<>0 OR NEW.capture_kind IS NOT NULL
   OR expected IS NULL OR NEW.source_snapshot<>expected::text
   OR NEW.output_watermark<>(expected->>'outputWatermark')::bigint
   OR NEW.acknowledged_watermark<>(expected->>'acknowledgedWatermark')::bigint
   OR NEW.stream_generation<>(expected->>'generation')::bigint
   OR NEW.terminal_digest<>expected->>'terminalDigest'
   OR NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_execution_streams stream
    WHERE stream.id=NEW.stream_id AND stream.organization_id=NEW.organization_id AND stream.capture_generation=NEW.capture_generation
     AND stream.closed_at<=NEW.sealed_at AND NEW.sealed_at<=clock_timestamp()) THEN
    RAISE EXCEPTION 'Kubernetes capture source is invalid' USING ERRCODE='23514';
  END IF;
 ELSE
  IF OLD.version=9223372036854775807 OR NEW.version<>OLD.version THEN
   RAISE EXCEPTION 'Kubernetes capture version is exhausted or caller assigned' USING ERRCODE='23514';
  END IF;
  IF ROW(NEW.repair_generation,NEW.repair_base_attempt_count) IS DISTINCT FROM ROW(OLD.repair_generation,OLD.repair_base_attempt_count) THEN
   IF OLD.state<>'WAITING_OWNER' OR NEW.state<>'READY' OR OLD.repair_generation=9223372036854775807
    OR NEW.repair_generation<>OLD.repair_generation+1 OR NEW.repair_base_attempt_count<>OLD.attempt_count
    OR NEW.attempt_count<>OLD.attempt_count OR NEW.generation<>OLD.generation OR NEW.reason_code<>''
    OR NEW.retain_until IS NOT NULL AND NEW.retain_until<=clock_timestamp()
    OR EXISTS(SELECT 1 FROM control_plane.kubernetes_capture_attempts attempt WHERE attempt.capture_id=OLD.id AND attempt.organization_id=OLD.organization_id AND attempt.state='ACTIVE')
    OR NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_execution_streams stream WHERE stream.id=OLD.stream_id AND stream.organization_id=OLD.organization_id AND stream.capture_generation=OLD.capture_generation) THEN
    RAISE EXCEPTION 'Kubernetes capture repair is invalid' USING ERRCODE='23514';
   END IF;
  ELSIF OLD.state='WAITING_OWNER' AND NEW.state='READY' THEN
   RAISE EXCEPTION 'Kubernetes capture repair cycle is missing' USING ERRCODE='23514';
  END IF;
  NEW.version:=OLD.version+1;
  IF ROW(NEW.id,NEW.ref,NEW.organization_id,NEW.stream_id,NEW.capture_generation,NEW.stream_generation,NEW.terminal_digest,NEW.source_snapshot,NEW.snapshot_digest,NEW.output_watermark,NEW.acknowledged_watermark,NEW.sealed_at,NEW.retain_until,NEW.created_at)
   IS DISTINCT FROM ROW(OLD.id,OLD.ref,OLD.organization_id,OLD.stream_id,OLD.capture_generation,OLD.stream_generation,OLD.terminal_digest,OLD.source_snapshot,OLD.snapshot_digest,OLD.output_watermark,OLD.acknowledged_watermark,OLD.sealed_at,OLD.retain_until,OLD.created_at)
   OR NEW.generation<OLD.generation OR NEW.attempt_count<OLD.attempt_count
   OR OLD.state='COMPLETE' AND ROW(NEW.state,NEW.capture_kind,NEW.source_digest,NEW.acknowledged_prefix,NEW.dropped_bytes) IS DISTINCT FROM ROW(OLD.state,OLD.capture_kind,OLD.source_digest,OLD.acknowledged_prefix,OLD.dropped_bytes) THEN
    RAISE EXCEPTION 'Kubernetes capture source is immutable' USING ERRCODE='23514';
  END IF;
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_kubernetes_output_capture() FROM PUBLIC;
CREATE TRIGGER kubernetes_output_capture_guard BEFORE INSERT OR UPDATE ON control_plane.kubernetes_output_captures
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_kubernetes_output_capture();
-- +goose StatementBegin
CREATE FUNCTION control_plane.enqueue_kubernetes_output_capture(p_stream uuid,p_org uuid) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE selected control_plane.kubernetes_execution_streams%ROWTYPE;
 previous control_plane.kubernetes_output_captures%ROWTYPE; source jsonb; sealed timestamptz; acknowledged bigint;
BEGIN
 SELECT * INTO selected FROM control_plane.kubernetes_execution_streams WHERE id=p_stream AND organization_id=p_org FOR UPDATE;
 IF NOT FOUND OR selected.state IN('PREPARED','ACTIVE') THEN RETURN; END IF;
 sealed:=clock_timestamp();
 SELECT coalesce(max(sequence),0) INTO acknowledged FROM control_plane.kubernetes_execution_frames
 WHERE stream_id=p_stream AND organization_id=p_org AND direction='OUTPUT' AND acknowledged_at<=sealed;
 SELECT * INTO previous FROM control_plane.kubernetes_output_captures WHERE stream_id=p_stream AND organization_id=p_org
 ORDER BY capture_generation DESC LIMIT 1;
 IF FOUND AND previous.acknowledged_watermark>=acknowledged
  AND previous.terminal_digest=coalesce(selected.terminal_digest,'') THEN RETURN; END IF;
 IF selected.capture_generation=9223372036854775807 THEN RAISE EXCEPTION 'Kubernetes capture generation is exhausted'; END IF;
 UPDATE control_plane.kubernetes_execution_streams SET capture_generation=capture_generation+1 WHERE id=p_stream AND organization_id=p_org;
 source:=control_plane.kubernetes_capture_source_snapshot(p_stream,p_org,sealed,selected.capture_generation+1,coalesce(previous.ref,''));
 IF source IS NULL THEN RAISE EXCEPTION 'Kubernetes capture origin is unavailable'; END IF;
 -- Publication уже принятой revision неизменна; незавершённый старый consumer теряет task grant.
 UPDATE control_plane.kubernetes_output_captures SET state='SUPERSEDED',reason_code='CAPTURE_SUPERSEDED'
 WHERE stream_id=p_stream AND organization_id=p_org AND state NOT IN('COMPLETE','SUPERSEDED');
 UPDATE control_plane.kubernetes_capture_attempts attempt SET state='CANCELLED',completed_at=sealed
 FROM control_plane.kubernetes_output_captures capture
 WHERE capture.id=attempt.capture_id AND capture.organization_id=attempt.organization_id
  AND capture.stream_id=p_stream AND capture.organization_id=p_org AND capture.state='SUPERSEDED' AND attempt.state='ACTIVE';
 INSERT INTO control_plane.kubernetes_output_captures(ref,organization_id,stream_id,capture_generation,stream_generation,
  terminal_digest,source_snapshot,snapshot_digest,output_watermark,acknowledged_watermark,sealed_at,retain_until)
 VALUES('koc_'||replace(gen_random_uuid()::text,'-',''),p_org,p_stream,selected.capture_generation+1,selected.generation,
  coalesce(selected.terminal_digest,''),source::text,encode(public.digest(convert_to(source::text,'UTF8'),'sha256'),'hex'),
  selected.output_sequence,acknowledged,sealed,NULL);
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.enqueue_kubernetes_output_capture(uuid,uuid) FROM PUBLIC,control_plane_runtime;
-- +goose StatementBegin
CREATE FUNCTION control_plane.trigger_kubernetes_output_capture() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF TG_RELID='control_plane.kubernetes_execution_streams'::regclass::oid THEN
  IF NEW.state NOT IN('PREPARED','ACTIVE') THEN PERFORM control_plane.enqueue_kubernetes_output_capture(NEW.id,NEW.organization_id); END IF;
 ELSIF NEW.direction='OUTPUT' AND NEW.acknowledged_at IS NOT NULL AND OLD.acknowledged_at IS NULL THEN
  PERFORM control_plane.enqueue_kubernetes_output_capture(NEW.stream_id,NEW.organization_id);
 END IF;
 RETURN NULL;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.trigger_kubernetes_output_capture() FROM PUBLIC;
CREATE TRIGGER kubernetes_capture_stream_terminal AFTER UPDATE OF state,terminal_digest ON control_plane.kubernetes_execution_streams
 FOR EACH ROW EXECUTE FUNCTION control_plane.trigger_kubernetes_output_capture();
CREATE TRIGGER kubernetes_capture_late_output_ack AFTER UPDATE OF acknowledged_at ON control_plane.kubernetes_execution_frames
 FOR EACH ROW EXECUTE FUNCTION control_plane.trigger_kubernetes_output_capture();
REVOKE ALL ON control_plane.kubernetes_output_captures,control_plane.kubernetes_capture_attempts FROM control_plane_runtime;
GRANT SELECT,INSERT,UPDATE ON control_plane.kubernetes_output_captures,control_plane.kubernetes_capture_attempts TO control_plane_runtime;
-- Capture task lease не является native invocation grant. Claim принадлежит
-- существующей immutable PostgreSQL identity artifact-retention, не caller GUC.
-- +goose StatementBegin
CREATE FUNCTION control_plane.kubernetes_capture_claim(p_owner text,p_limit integer,p_lease integer)
RETURNS TABLE(capture_ref text,lease_ref text,fence text,generation bigint,snapshot_digest text,expires_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE item control_plane.kubernetes_output_captures%ROWTYPE; raw_fence text; attempt_ref text; deadline timestamptz;
BEGIN
 IF NOT pg_has_role(session_user,'artifact_retention_runtime','MEMBER')
  OR NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=session_user AND rolcanlogin)
  OR p_owner IS NULL OR p_limit IS NULL OR p_lease IS NULL OR char_length(p_owner) NOT BETWEEN 1 AND 128
  OR p_owner ~ '[[:space:][:cntrl:]]' OR p_limit NOT BETWEEN 1 AND 100 OR p_lease NOT BETWEEN 30 AND 300 THEN
  RAISE EXCEPTION 'Kubernetes capture claim is invalid';
 END IF;
 FOR item IN SELECT source.* FROM control_plane.kubernetes_output_captures source
  WHERE source.state IN('READY','CLAIMED') AND source.next_attempt_at<=clock_timestamp()
   AND NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_capture_attempts attempt
    WHERE attempt.capture_id=source.id AND attempt.organization_id=source.organization_id AND attempt.state='ACTIVE' AND attempt.expires_at>clock_timestamp())
  ORDER BY source.next_attempt_at,source.id LIMIT p_limit FOR UPDATE SKIP LOCKED
 LOOP
  UPDATE control_plane.kubernetes_capture_attempts SET state='EXPIRED',completed_at=clock_timestamp()
   WHERE capture_id=item.id AND organization_id=item.organization_id AND state='ACTIVE' AND kubernetes_capture_attempts.expires_at<=clock_timestamp();
  IF NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_execution_streams stream
   WHERE stream.id=item.stream_id AND stream.organization_id=item.organization_id AND stream.capture_generation=item.capture_generation) THEN
   UPDATE control_plane.kubernetes_output_captures SET state='SUPERSEDED',reason_code='CAPTURE_SUPERSEDED' WHERE id=item.id;
   CONTINUE;
  END IF;
  IF item.attempt_count-item.repair_base_attempt_count>=3 OR item.attempt_count=9223372036854775807 OR item.generation=9223372036854775807 THEN
   UPDATE control_plane.kubernetes_output_captures SET state='WAITING_OWNER',reason_code='CAPTURE_RETRY_EXHAUSTED' WHERE id=item.id;
   CONTINUE;
  END IF;
  IF item.retain_until IS NOT NULL AND item.retain_until<=clock_timestamp() THEN
   UPDATE control_plane.kubernetes_output_captures SET state='REJECTED',reason_code='CAPTURE_RETENTION_EXPIRED' WHERE id=item.id;
   CONTINUE;
  END IF;
  raw_fence:='capture-'||replace(gen_random_uuid()::text,'-','')||replace(gen_random_uuid()::text,'-','');
  attempt_ref:='kca_'||replace(gen_random_uuid()::text,'-','');
  deadline:=clock_timestamp()+p_lease*interval '1 second';
  UPDATE control_plane.kubernetes_output_captures SET state='CLAIMED',generation=kubernetes_output_captures.generation+1,
   attempt_count=attempt_count+1,reason_code='' WHERE id=item.id;
  INSERT INTO control_plane.kubernetes_capture_attempts(ref,organization_id,capture_id,generation,workload_instance,fence_digest,expires_at,hard_deadline,state)
  VALUES(attempt_ref,item.organization_id,item.id,item.generation+1,p_owner,encode(public.digest(convert_to(raw_fence,'UTF8'),'sha256'),'hex'),deadline,clock_timestamp()+interval '10 minutes','ACTIVE');
  capture_ref:=item.ref; lease_ref:=attempt_ref; fence:=raw_fence; generation:=item.generation+1; snapshot_digest:=item.snapshot_digest; expires_at:=deadline;
  RETURN NEXT;
 END LOOP;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.kubernetes_capture_claim(text,integer,integer) FROM PUBLIC,control_plane_runtime;
GRANT EXECUTE ON FUNCTION control_plane.kubernetes_capture_claim(text,integer,integer) TO artifact_retention_runtime;
-- Совместимость consumer назначается живой PostgreSQL identity, не instance из payload.
-- Readiness producer не требует heartbeat, иначе startup образовал бы цикл.
CREATE TABLE control_plane.kubernetes_capture_consumers (
 session_name name NOT NULL,
 workload_instance text NOT NULL CHECK(octet_length(workload_instance) BETWEEN 1 AND 128 AND workload_instance !~ '[[:space:][:cntrl:]]'),
 contract_revision integer NOT NULL CHECK(contract_revision=1),
 checked_at timestamptz NOT NULL,
 PRIMARY KEY(session_name,workload_instance)
);
CREATE INDEX kubernetes_capture_consumers_checked ON control_plane.kubernetes_capture_consumers(checked_at);
REVOKE ALL ON control_plane.kubernetes_capture_consumers FROM PUBLIC,control_plane_runtime,artifact_retention_runtime;
GRANT SELECT ON control_plane.kubernetes_capture_consumers TO control_plane_runtime;
-- +goose StatementBegin
CREATE FUNCTION control_plane.kubernetes_capture_consumer_heartbeat(p_instance text,p_revision integer)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF NOT pg_has_role(session_user,'artifact_retention_runtime','MEMBER')
  OR NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=session_user AND rolcanlogin)
  OR p_revision IS DISTINCT FROM 1 OR p_instance IS NULL OR octet_length(p_instance) NOT BETWEEN 1 AND 128
  OR p_instance ~ '[[:space:][:cntrl:]]' THEN RETURN false; END IF;
 INSERT INTO control_plane.kubernetes_capture_consumers(session_name,workload_instance,contract_revision,checked_at)
 VALUES(session_user,p_instance,p_revision,clock_timestamp())
 ON CONFLICT(session_name,workload_instance) DO UPDATE SET contract_revision=EXCLUDED.contract_revision,checked_at=EXCLUDED.checked_at;
 DELETE FROM control_plane.kubernetes_capture_consumers consumer WHERE(consumer.session_name,consumer.workload_instance) IN(
  SELECT stale.session_name,stale.workload_instance FROM control_plane.kubernetes_capture_consumers stale
  WHERE stale.checked_at<clock_timestamp()-interval '10 minutes' ORDER BY stale.checked_at LIMIT 100);
 RETURN true;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.kubernetes_capture_consumer_heartbeat(text,integer) FROM PUBLIC,control_plane_runtime;
GRANT EXECUTE ON FUNCTION control_plane.kubernetes_capture_consumer_heartbeat(text,integer) TO artifact_retention_runtime;
-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_kubernetes_capture_attempt() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.state<>'ACTIVE' OR NEW.completed_at IS NOT NULL OR NOT EXISTS(
   SELECT 1 FROM control_plane.kubernetes_output_captures capture
   WHERE capture.id=NEW.capture_id AND capture.organization_id=NEW.organization_id AND capture.state='CLAIMED'
    AND capture.generation=NEW.generation AND capture.attempt_count=NEW.generation) THEN
   RAISE EXCEPTION 'Kubernetes capture attempt source is invalid' USING ERRCODE='23514';
  END IF;
 ELSE
  IF ROW(NEW.id,NEW.ref,NEW.organization_id,NEW.capture_id,NEW.generation,NEW.workload_instance,NEW.fence_digest,NEW.hard_deadline,NEW.created_at)
   IS DISTINCT FROM ROW(OLD.id,OLD.ref,OLD.organization_id,OLD.capture_id,OLD.generation,OLD.workload_instance,OLD.fence_digest,OLD.hard_deadline,OLD.created_at)
   OR OLD.state<>'ACTIVE' AND ROW(NEW.state,NEW.expires_at,NEW.completed_at) IS DISTINCT FROM ROW(OLD.state,OLD.expires_at,OLD.completed_at)
   OR NEW.expires_at<OLD.expires_at OR OLD.completed_at IS NOT NULL AND NEW.completed_at IS DISTINCT FROM OLD.completed_at THEN
   RAISE EXCEPTION 'Kubernetes capture attempt origin is immutable' USING ERRCODE='23514';
  END IF;
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_kubernetes_capture_attempt() FROM PUBLIC;
CREATE TRIGGER kubernetes_capture_attempt_guard BEFORE INSERT OR UPDATE ON control_plane.kubernetes_capture_attempts
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_kubernetes_capture_attempt();
-- Capture принимает canonical Artifact/Revision; частные source bytes остаются в прежнем stage lifecycle.
CREATE TABLE control_plane.kubernetes_capture_artifact_heads (
 organization_id uuid NOT NULL,
 stream_id uuid NOT NULL,
 channel text NOT NULL CHECK(channel IN('CONTROL','STDOUT','STDERR','PORT_FORWARD','FILE_CONTENT')),
 artifact_ref text NOT NULL UNIQUE CHECK(artifact_ref ~ '^art_[A-Za-z0-9_-]{8,89}$'),
 artifact_id uuid REFERENCES control_plane.artifact_heads(id) DEFERRABLE INITIALLY IMMEDIATE,
 PRIMARY KEY(stream_id,channel),
 UNIQUE(stream_id,channel,organization_id),
 FOREIGN KEY(stream_id,organization_id) REFERENCES control_plane.kubernetes_execution_streams(id,organization_id) DEFERRABLE INITIALLY IMMEDIATE
);
ALTER TABLE control_plane.kubernetes_output_captures ADD CONSTRAINT kubernetes_capture_stream_tenant UNIQUE(id,stream_id,organization_id);
CREATE TABLE control_plane.kubernetes_capture_artifact_revisions (
 organization_id uuid NOT NULL,
 capture_id uuid NOT NULL,
 stream_id uuid NOT NULL,
 channel text NOT NULL,
 artifact_id uuid NOT NULL,
 revision_id uuid,
 artifact_ref text NOT NULL,
 revision_ref text NOT NULL,
 digest text NOT NULL CHECK(digest ~ '^sha256:[a-f0-9]{64}$'),
 size_bytes bigint NOT NULL CHECK(size_bytes BETWEEN 0 AND 536870912),
 prepared_content_id uuid NOT NULL REFERENCES control_plane.prepared_content(id) DEFERRABLE INITIALLY IMMEDIATE,
 source_digest text NOT NULL CHECK(source_digest ~ '^[a-f0-9]{64}$'),
 origin_json text NOT NULL CHECK(jsonb_typeof(origin_json::jsonb)='object' AND octet_length(origin_json)<=16384),
 published_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(capture_id,channel),UNIQUE(revision_id),
 FOREIGN KEY(capture_id,stream_id,organization_id) REFERENCES control_plane.kubernetes_output_captures(id,stream_id,organization_id) DEFERRABLE INITIALLY IMMEDIATE,
 FOREIGN KEY(stream_id,channel,organization_id) REFERENCES control_plane.kubernetes_capture_artifact_heads(stream_id,channel,organization_id) DEFERRABLE INITIALLY IMMEDIATE,
 FOREIGN KEY(artifact_id,revision_id) REFERENCES control_plane.artifact_revisions(artifact_id,id) ON DELETE SET NULL(revision_id) DEFERRABLE INITIALLY IMMEDIATE
);
-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_kubernetes_capture_artifact() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF TG_OP='DELETE' THEN
  IF control_plane.project_purge_authorized() THEN RETURN OLD; END IF;
  RAISE EXCEPTION 'Kubernetes capture artifact source is immutable' USING ERRCODE='23514';
 END IF;
 IF TG_TABLE_NAME='kubernetes_capture_artifact_heads' THEN
  IF TG_OP='UPDATE' AND((to_jsonb(NEW)-'artifact_id') IS DISTINCT FROM(to_jsonb(OLD)-'artifact_id')
   OR OLD.artifact_id IS NOT NULL AND NEW.artifact_id IS DISTINCT FROM OLD.artifact_id) THEN
   RAISE EXCEPTION 'Kubernetes capture artifact identity is immutable' USING ERRCODE='23514';
  END IF;
  IF NEW.artifact_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM control_plane.artifact_heads artifact
   WHERE artifact.id=NEW.artifact_id AND artifact.ref=NEW.artifact_ref AND artifact.organization_id=NEW.organization_id) THEN
   RAISE EXCEPTION 'Kubernetes capture artifact identity conflicts' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF TG_OP='UPDATE' THEN
  IF(to_jsonb(NEW)-'revision_id') IS DISTINCT FROM(to_jsonb(OLD)-'revision_id')
   OR NEW.revision_id IS NOT NULL OR NOT EXISTS(SELECT 1 FROM control_plane.artifact_heads artifact
    WHERE artifact.id=OLD.artifact_id AND artifact.organization_id=OLD.organization_id AND artifact.lifecycle_state='PURGED') THEN
   RAISE EXCEPTION 'Kubernetes capture artifact receipt is immutable' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_output_captures capture
  JOIN control_plane.kubernetes_execution_streams stream ON stream.id=capture.stream_id AND stream.organization_id=capture.organization_id
  JOIN control_plane.kubernetes_capture_attempts attempt ON attempt.capture_id=capture.id AND attempt.organization_id=capture.organization_id
  JOIN control_plane.prepared_content prepared ON prepared.id=NEW.prepared_content_id AND prepared.organization_id=capture.organization_id
  JOIN control_plane.kubernetes_capture_artifact_heads output ON output.stream_id=capture.stream_id AND output.organization_id=capture.organization_id AND output.channel=NEW.channel
  JOIN control_plane.artifact_heads artifact ON artifact.id=NEW.artifact_id AND artifact.organization_id=capture.organization_id
  JOIN control_plane.artifact_revisions revision ON revision.id=NEW.revision_id AND revision.artifact_id=artifact.id
  JOIN control_plane.artifact_revision_content content ON content.revision_id=revision.id
  WHERE capture.id=NEW.capture_id AND capture.organization_id=NEW.organization_id AND capture.stream_id=NEW.stream_id
   AND capture.state='CLAIMED' AND capture.generation=attempt.generation AND attempt.state='ACTIVE'
   AND attempt.expires_at>clock_timestamp() AND attempt.hard_deadline>clock_timestamp()
   AND stream.capture_generation=capture.capture_generation
   AND output.artifact_id=artifact.id AND output.artifact_ref=artifact.ref AND artifact.ref=NEW.artifact_ref AND revision.ref=NEW.revision_ref
   AND prepared.state='STAGED' AND prepared.source_profile='KUBERNETES_OUTPUT' AND prepared.source_profile_ref=capture.ref
   AND prepared.source_profile_version=capture.capture_generation AND prepared.source_context_digest=capture.snapshot_digest
   AND prepared.operation_key='kubernetes.output.'||NEW.channel AND prepared.prepared_artifact_ref=artifact.ref AND prepared.prepared_revision_ref=revision.ref
   AND content.object_key=prepared.object_key AND content.object_version=prepared.object_version AND content.object_etag=prepared.object_etag
   AND content.digest=NEW.digest AND content.size_bytes=NEW.size_bytes AND prepared.digest=NEW.digest AND prepared.size_bytes=NEW.size_bytes
   AND NEW.origin_json::jsonb->>'invocationRef'=capture.source_snapshot::jsonb->>'invocationRef'
   AND NEW.origin_json::jsonb->>'streamRef'=stream.ref
   AND(NEW.origin_json::jsonb->>'streamGeneration')::bigint=capture.stream_generation
   AND(NEW.origin_json::jsonb->>'captureGeneration')::bigint=capture.capture_generation
   AND NEW.origin_json::jsonb->>'channel'=NEW.channel AND NEW.origin_json::jsonb->>'sourceDigest'=NEW.source_digest
   AND(NEW.origin_json::jsonb->>'acknowledgedPrefix')::bigint<=capture.acknowledged_watermark
   AND(NEW.origin_json::jsonb->>'outputWatermark')::bigint=capture.output_watermark
   AND(NEW.origin_json::jsonb->>'sealedAt')::timestamptz=capture.sealed_at
 ) THEN RAISE EXCEPTION 'Kubernetes capture artifact provenance is invalid' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_kubernetes_capture_artifact() FROM PUBLIC;
CREATE TRIGGER kubernetes_capture_artifact_head_guard BEFORE INSERT OR UPDATE OR DELETE ON control_plane.kubernetes_capture_artifact_heads
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_kubernetes_capture_artifact();
CREATE TRIGGER kubernetes_capture_artifact_revision_guard BEFORE INSERT OR UPDATE OR DELETE ON control_plane.kubernetes_capture_artifact_revisions
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_kubernetes_capture_artifact();
REVOKE ALL ON control_plane.kubernetes_capture_artifact_heads,control_plane.kubernetes_capture_artifact_revisions FROM control_plane_runtime;
GRANT SELECT,INSERT,UPDATE ON control_plane.kubernetes_capture_artifact_heads TO control_plane_runtime;
GRANT SELECT,INSERT ON control_plane.kubernetes_capture_artifact_revisions TO control_plane_runtime;
-- +goose StatementBegin
CREATE FUNCTION control_plane.kubernetes_capture_artifact_visible(p_org uuid,p_actor uuid,p_artifact uuid) RETURNS boolean
LANGUAGE sql STABLE SET search_path=pg_catalog AS $$
 SELECT NOT EXISTS(
  SELECT 1 FROM control_plane.kubernetes_capture_artifact_heads head
  JOIN control_plane.kubernetes_capture_artifact_revisions revision ON revision.stream_id=head.stream_id
   AND revision.channel=head.channel AND revision.organization_id=head.organization_id
  JOIN control_plane.kubernetes_output_captures capture ON capture.id=revision.capture_id AND capture.organization_id=revision.organization_id
  WHERE head.organization_id=p_org AND head.artifact_id=p_artifact
   AND NOT coalesce((capture.source_snapshot::jsonb->>'credentialPolicy'='INITIATOR_REQUIRED'
     AND capture.source_snapshot::jsonb->>'subjectKind'='USER'
     AND capture.source_snapshot::jsonb->>'rootActorId'=p_actor::text
     AND capture.source_snapshot::jsonb->>'subjectId'=p_actor::text)
    OR(capture.source_snapshot::jsonb->>'credentialPolicy'='PROJECT_SHARED'
     AND capture.source_snapshot::jsonb->>'subjectKind'='SERVICE'),false)
 );
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.kubernetes_capture_artifact_visible(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.kubernetes_capture_artifact_visible(uuid,uuid,uuid) TO control_plane_runtime;
-- Сохраняем OID существующего canonical predicate, включая все зависимости SQL functions/views.
-- +goose StatementBegin
DO $capture_visibility$
DECLARE definition text; marker text;
BEGIN
 SELECT pg_get_functiondef('control_plane.catalog_resource_visible(uuid,uuid,text,text,uuid,uuid,uuid,jsonb,timestamptz,boolean)'::regprocedure) INTO definition;
 marker:='WHERE binding.organization_id = tenant AND binding.state = ''ACTIVE''';
 IF (char_length(definition)-char_length(replace(definition,marker,'')))/char_length(marker)<>1 THEN
  RAISE EXCEPTION 'Kubernetes capture visibility predecessor is unexpected';
 END IF;
 definition:=replace(definition,marker,marker||E'\n          AND (target_kind<>''ARTIFACT'' OR (control_plane.kubernetes_capture_artifact_visible(tenant,actor,target_id) AND (permission<>''artifact.revision.create'' OR NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_capture_artifact_heads capture_head WHERE capture_head.organization_id=tenant AND capture_head.artifact_id=target_id))))');
 EXECUTE definition;
END $capture_visibility$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION control_plane.kubernetes_capture_runtime_visible(p_org uuid,p_actor uuid,p_artifact uuid,p_node uuid,p_purpose text) RETURNS boolean
LANGUAGE sql STABLE SET search_path=pg_catalog AS $$
 SELECT NOT EXISTS(
  SELECT 1 FROM control_plane.kubernetes_capture_artifact_heads head
  JOIN control_plane.kubernetes_capture_artifact_revisions revision ON revision.stream_id=head.stream_id
   AND revision.channel=head.channel AND revision.organization_id=head.organization_id
  JOIN control_plane.kubernetes_output_captures capture ON capture.id=revision.capture_id AND capture.organization_id=revision.organization_id
  WHERE head.organization_id=p_org AND head.artifact_id=p_artifact
   AND NOT coalesce((capture.source_snapshot::jsonb->>'credentialPolicy'='PROJECT_SHARED'
      AND capture.source_snapshot::jsonb->>'subjectKind'='SERVICE')
    OR(capture.source_snapshot::jsonb->>'credentialPolicy'='INITIATOR_REQUIRED'
     AND capture.source_snapshot::jsonb->>'rootActorId'=p_actor::text
     AND capture.source_snapshot::jsonb->>'subjectId'=p_actor::text
     AND capture.source_snapshot::jsonb->>'subjectKind'='USER'
     AND(p_purpose='WORKSPACE_INPUT' OR EXISTS(SELECT 1 FROM control_plane.run_nodes node
      WHERE node.id=p_node AND node.organization_id=p_org
       AND node.root_run_id::text=capture.source_snapshot::jsonb->>'rootRunId'))),false)
 );
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.kubernetes_capture_runtime_visible(uuid,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.kubernetes_capture_runtime_visible(uuid,uuid,uuid,uuid,text) TO control_plane_runtime;
-- +goose StatementBegin
DO $capture_runtime_visibility$
DECLARE definition text; marker text;
BEGIN
 SELECT pg_get_functiondef('control_plane.runtime_file_source_visible(uuid,uuid,uuid,uuid,uuid,text,text,uuid)'::regprocedure) INTO definition;
 marker:='WHERE file_target.organization_id=p_tenant AND file_target.kind=''ARTIFACT'' AND file.id=p_artifact';
 IF (char_length(definition)-char_length(replace(definition,marker,'')))/char_length(marker)<>1 THEN
  RAISE EXCEPTION 'Kubernetes capture runtime visibility predecessor is unexpected';
 END IF;
 definition:=replace(definition,marker,marker||E'\n          AND control_plane.kubernetes_capture_runtime_visible(p_tenant,p_actor,p_artifact,p_node,p_purpose)');
 EXECUTE definition;
END $capture_runtime_visibility$;
-- +goose StatementEnd

ALTER TABLE control_plane.prepared_content DROP CONSTRAINT prepared_content_source_profile_check;
ALTER TABLE control_plane.prepared_content ADD CONSTRAINT prepared_content_source_profile_check CHECK(source_profile IN('SYSTEM','PROJECT','KUBERNETES_OUTPUT'));
ALTER TABLE control_plane.prepared_content DROP CONSTRAINT prepared_content_size_bytes_check;
ALTER TABLE control_plane.prepared_content ADD CONSTRAINT prepared_content_size_bytes_check CHECK(size_bytes>=0 AND size_bytes<=CASE WHEN source_profile='KUBERNETES_OUTPUT' THEN 536870912 ELSE 1048576 END);
-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_kubernetes_capture_prepared() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF NEW.source_profile<>'KUBERNETES_OUTPUT' THEN RETURN NEW; END IF;
 IF NEW.intent_operation<>'kubernetes.capture.finalize' OR NEW.operation_key NOT IN('kubernetes.output.STDOUT','kubernetes.output.STDERR','kubernetes.output.CONTROL','kubernetes.output.PORT_FORWARD','kubernetes.output.FILE_CONTENT')
  OR NEW.idempotency_key<>NEW.source_profile_ref||':'||substring(NEW.operation_key FROM 19)
  OR NEW.available_until<>'infinity'::timestamptz OR NOT EXISTS(
   SELECT 1 FROM control_plane.kubernetes_output_captures capture
   JOIN control_plane.kubernetes_execution_streams stream ON stream.id=capture.stream_id AND stream.organization_id=capture.organization_id
   JOIN control_plane.kubernetes_capture_attempts attempt ON attempt.capture_id=capture.id AND attempt.organization_id=capture.organization_id
   JOIN control_plane.kubernetes_capture_artifact_heads output ON output.stream_id=capture.stream_id AND output.organization_id=capture.organization_id
   WHERE capture.ref=NEW.source_profile_ref AND capture.organization_id=NEW.organization_id
    AND capture.state='CLAIMED' AND capture.generation=attempt.generation AND attempt.state='ACTIVE'
    AND attempt.expires_at>clock_timestamp() AND attempt.hard_deadline>clock_timestamp()
    AND stream.capture_generation=capture.capture_generation AND capture.capture_generation=NEW.source_profile_version
    AND NEW.size_bytes<=(capture.source_snapshot::jsonb->>'maximumTotalBytes')::bigint
    AND capture.snapshot_digest=NEW.source_context_digest
    AND capture.source_snapshot::jsonb->>'rootActorId'=NEW.actor_id::text
    AND coalesce(capture.source_snapshot::jsonb->>'projectId','')=coalesce(NEW.project_id::text,'')
    AND capture.source_snapshot::jsonb->>'projectRef'=NEW.origin_project_ref
    AND capture.source_snapshot::jsonb->>'originalLeaseRef'=NEW.source_lease_ref
    AND (capture.source_snapshot::jsonb->>'generation')::bigint=NEW.source_lease_generation
    AND capture.source_snapshot::jsonb->>'originalFenceDigest'=NEW.source_fence_digest
    AND capture.source_snapshot::jsonb->>'runRef'=NEW.source_run_ref
    AND output.channel=substring(NEW.operation_key FROM 19) AND output.artifact_ref=NEW.prepared_artifact_ref
    AND output.artifact_ref=NEW.target_artifact_ref AND output.artifact_id IS NOT DISTINCT FROM NEW.target_artifact_id
  ) THEN RAISE EXCEPTION 'Kubernetes prepared capture origin is invalid' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_kubernetes_capture_prepared() FROM PUBLIC;
CREATE TRIGGER kubernetes_capture_prepared_origin BEFORE INSERT ON control_plane.prepared_content
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_kubernetes_capture_prepared();

-- Старый prepared consumer сохраняет прежний 1MiB-контракт и не забирает KUBE intent.
-- +goose StatementBegin
DO $capture_prepared_cleanup$
DECLARE definition text; marker text;
BEGIN
 SELECT pg_get_functiondef('control_plane.prepared_content_claim(text,integer,integer)'::regprocedure) INTO definition;
 marker:='WHERE candidate.state IN (''IN_FLIGHT'',''UNKNOWN'',''STAGED'',''ABANDONED'',''CLEANING'')';
 IF(char_length(definition)-char_length(replace(definition,marker,'')))/char_length(marker)<>1 THEN
  RAISE EXCEPTION 'Kubernetes prepared cleanup predecessor is unexpected'; END IF;
 EXECUTE replace(definition,marker,marker||E'\n        AND candidate.source_profile IN (''SYSTEM'',''PROJECT'')');
END $capture_prepared_cleanup$;
-- +goose StatementEnd

-- Repair относится к cleanup cycle; native и capture attempt counters не меняются.
ALTER TABLE control_plane.kubernetes_execution_streams
 ADD COLUMN cleanup_version bigint NOT NULL DEFAULT 1 CHECK(cleanup_version>0),
 ADD COLUMN cleanup_repair_generation bigint NOT NULL DEFAULT 0 CHECK(cleanup_repair_generation>=0);
ALTER TABLE control_plane.prepared_content
 ADD COLUMN cleanup_total_attempts bigint NOT NULL DEFAULT 0 CHECK(cleanup_total_attempts>=0),
 ADD COLUMN cleanup_repair_generation bigint NOT NULL DEFAULT 0 CHECK(cleanup_repair_generation>=0);
-- +goose StatementBegin
DO $kubernetes_prepared_cleanup_metadata$
DECLARE definition text; marker text := '''cleanup_attempts'',''available_until'',''updated_at''';
BEGIN
 SELECT pg_get_functiondef('control_plane.protect_prepared_content()'::regprocedure) INTO definition;
 IF (length(definition)-length(replace(definition,marker,'')))/length(marker)<>2 THEN
  RAISE EXCEPTION 'prepared cleanup metadata marker mismatch'; END IF;
 EXECUTE replace(definition,marker,'''cleanup_attempts'',''cleanup_total_attempts'',''cleanup_repair_generation'',''available_until'',''updated_at''');
END $kubernetes_prepared_cleanup_metadata$;
-- +goose StatementEnd

-- Факты frame/idem остаются неизменными после подтверждённого удаления payload.
ALTER TABLE control_plane.kubernetes_input_stages
 ADD COLUMN cleanup_generation bigint NOT NULL DEFAULT 0 CHECK(cleanup_generation>=0),
 ADD COLUMN cleanup_owner text,
 ADD COLUMN cleanup_deadline timestamptz,
 ADD COLUMN cleanup_attempts integer NOT NULL DEFAULT 0 CHECK(cleanup_attempts BETWEEN 0 AND 3),
 ADD COLUMN cleanup_total_attempts bigint NOT NULL DEFAULT 0 CHECK(cleanup_total_attempts>=cleanup_attempts),
 ADD COLUMN cleanup_repair_generation bigint NOT NULL DEFAULT 0 CHECK(cleanup_repair_generation>=0),
 ADD COLUMN cleanup_object_version text,
 ADD COLUMN cleanup_object_etag text,
 ADD COLUMN payload_purged_at timestamptz,
 ADD COLUMN deletion_receipt_digest text CHECK(deletion_receipt_digest ~ '^[a-f0-9]{64}$'),
 ADD CHECK((cleanup_owner IS NULL)=(cleanup_deadline IS NULL)),
 ADD CHECK((cleanup_object_version IS NULL)=(cleanup_object_etag IS NULL)),
 ADD CHECK((payload_purged_at IS NULL)=(deletion_receipt_digest IS NULL));
ALTER TABLE control_plane.kubernetes_output_stages
 ADD COLUMN cleanup_generation bigint NOT NULL DEFAULT 0 CHECK(cleanup_generation>=0),
 ADD COLUMN cleanup_owner text,
 ADD COLUMN cleanup_deadline timestamptz,
 ADD COLUMN cleanup_attempts integer NOT NULL DEFAULT 0 CHECK(cleanup_attempts BETWEEN 0 AND 3),
 ADD COLUMN cleanup_total_attempts bigint NOT NULL DEFAULT 0 CHECK(cleanup_total_attempts>=cleanup_attempts),
 ADD COLUMN cleanup_repair_generation bigint NOT NULL DEFAULT 0 CHECK(cleanup_repair_generation>=0),
 ADD COLUMN cleanup_object_version text,
 ADD COLUMN cleanup_object_etag text,
 ADD COLUMN payload_purged_at timestamptz,
 ADD COLUMN deletion_receipt_digest text CHECK(deletion_receipt_digest ~ '^[a-f0-9]{64}$'),
 ADD CHECK((cleanup_owner IS NULL)=(cleanup_deadline IS NULL)),
 ADD CHECK((cleanup_object_version IS NULL)=(cleanup_object_etag IS NULL)),
 ADD CHECK((payload_purged_at IS NULL)=(deletion_receipt_digest IS NULL));
CREATE INDEX kubernetes_input_stage_cleanup ON control_plane.kubernetes_input_stages(created_at,id) WHERE payload_purged_at IS NULL;
CREATE INDEX kubernetes_output_stage_cleanup ON control_plane.kubernetes_output_stages(created_at,id) WHERE payload_purged_at IS NULL;
CREATE INDEX kubernetes_input_stage_storage_quota ON control_plane.kubernetes_input_stages(organization_id) INCLUDE(byte_count) WHERE payload_purged_at IS NULL;
CREATE INDEX kubernetes_output_stage_storage_quota ON control_plane.kubernetes_output_stages(organization_id) INCLUDE(byte_count) WHERE payload_purged_at IS NULL;
-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_kubernetes_stage_cleanup() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.cleanup_generation<>0 OR NEW.cleanup_owner IS NOT NULL OR NEW.cleanup_deadline IS NOT NULL
   OR NEW.cleanup_attempts<>0 OR NEW.cleanup_total_attempts<>0 OR NEW.cleanup_repair_generation<>0 OR NEW.cleanup_object_version IS NOT NULL OR NEW.cleanup_object_etag IS NOT NULL
   OR NEW.payload_purged_at IS NOT NULL OR NEW.deletion_receipt_digest IS NOT NULL THEN
   RAISE EXCEPTION 'Kubernetes stage cleanup cannot be self-issued' USING ERRCODE='23514'; END IF;
 ELSE
  IF ROW(NEW.cleanup_generation,NEW.cleanup_owner,NEW.cleanup_deadline,NEW.cleanup_attempts,NEW.cleanup_total_attempts,NEW.cleanup_repair_generation,NEW.cleanup_object_version,NEW.cleanup_object_etag,NEW.payload_purged_at,NEW.deletion_receipt_digest)
   IS DISTINCT FROM ROW(OLD.cleanup_generation,OLD.cleanup_owner,OLD.cleanup_deadline,OLD.cleanup_attempts,OLD.cleanup_total_attempts,OLD.cleanup_repair_generation,OLD.cleanup_object_version,OLD.cleanup_object_etag,OLD.payload_purged_at,OLD.deletion_receipt_digest)
   AND(current_user<>'control_plane_owner' OR NEW.cleanup_generation<OLD.cleanup_generation
    OR OLD.payload_purged_at IS NOT NULL OR OLD.cleanup_object_version IS NOT NULL
     AND ROW(NEW.cleanup_object_version,NEW.cleanup_object_etag) IS DISTINCT FROM ROW(OLD.cleanup_object_version,OLD.cleanup_object_etag)) THEN
   RAISE EXCEPTION 'Kubernetes stage cleanup receipt is immutable' USING ERRCODE='23514'; END IF;
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_kubernetes_stage_cleanup() FROM PUBLIC;
CREATE TRIGGER kubernetes_input_stage_cleanup_guard BEFORE INSERT OR UPDATE ON control_plane.kubernetes_input_stages FOR EACH ROW EXECUTE FUNCTION control_plane.guard_kubernetes_stage_cleanup();
CREATE TRIGGER kubernetes_output_stage_cleanup_guard BEFORE INSERT OR UPDATE ON control_plane.kubernetes_output_stages FOR EACH ROW EXECUTE FUNCTION control_plane.guard_kubernetes_stage_cleanup();

-- +goose StatementBegin
CREATE FUNCTION control_plane.kubernetes_stage_cleanup_eligible(p_kind text,p_id uuid,p_stream uuid)
RETURNS boolean LANGUAGE plpgsql VOLATILE SET search_path=pg_catalog AS $$
DECLARE terminal boolean;
BEGIN
 SELECT state NOT IN('PREPARED','ACTIVE') INTO terminal FROM control_plane.kubernetes_execution_streams WHERE id=p_stream;
 IF p_kind='KUBERNETES_INPUT' THEN
  RETURN EXISTS(SELECT 1 FROM control_plane.kubernetes_input_stages stage WHERE stage.id=p_id AND stage.stream_id=p_stream AND stage.channel<>'FILE_CONTENT'
   AND(stage.state='ACKNOWLEDGED' OR terminal AND stage.state IN('CANCELLED','UNKNOWN','REJECTED')));
 ELSIF p_kind='KUBERNETES_OUTPUT' THEN
  RETURN terminal AND EXISTS(SELECT 1 FROM control_plane.kubernetes_output_stages stage WHERE stage.id=p_id AND stage.stream_id=p_stream
   AND(stage.state<>'ACCEPTED' OR EXISTS(
    SELECT 1 FROM control_plane.kubernetes_output_captures capture
    JOIN control_plane.kubernetes_execution_streams stream ON stream.id=capture.stream_id AND stream.organization_id=capture.organization_id
    WHERE capture.stream_id=stage.stream_id AND capture.organization_id=stage.organization_id
     AND capture.capture_generation=stream.capture_generation AND capture.state='COMPLETE'
     AND stage.sequence<=capture.acknowledged_prefix
     AND EXISTS(SELECT 1 FROM control_plane.kubernetes_capture_artifact_revisions saved
      JOIN control_plane.artifact_heads artifact ON artifact.id=saved.artifact_id AND artifact.organization_id=saved.organization_id
      LEFT JOIN control_plane.artifact_revision_content content ON content.revision_id=saved.revision_id
      WHERE saved.capture_id=capture.id AND saved.organization_id=capture.organization_id AND saved.channel=stage.channel
       AND(content.revision_id IS NOT NULL OR artifact.lifecycle_state='PURGED' AND artifact.purged_at IS NOT NULL))
   )));
 ELSIF p_kind='KUBERNETES_PREPARED' THEN
  RETURN terminal AND EXISTS(SELECT 1 FROM control_plane.prepared_content prepared
   JOIN control_plane.kubernetes_output_captures capture ON capture.ref=prepared.source_profile_ref AND capture.organization_id=prepared.organization_id
   WHERE prepared.id=p_id AND prepared.source_profile='KUBERNETES_OUTPUT' AND capture.stream_id=p_stream
    AND capture.state IN('SUPERSEDED','REJECTED','COMPLETE') AND prepared.adopted_revision_id IS NULL AND prepared.state<>'ADOPTED'
    AND NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_capture_attempts attempt
     WHERE attempt.capture_id=capture.id AND attempt.state='ACTIVE' AND attempt.expires_at>clock_timestamp()));
 END IF;
 RETURN false;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.kubernetes_stage_cleanup_eligible(text,uuid,uuid) FROM PUBLIC,control_plane_runtime,artifact_retention_runtime;

-- +goose StatementBegin
CREATE FUNCTION control_plane.kubernetes_stage_cleanup_claim(p_owner text,p_limit integer,p_lease integer)
RETURNS TABLE(source_kind text,ledger_id uuid,object_key text,object_version text,object_etag text,digest text,size_bytes bigint,generation bigint,uncertain boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE candidate record; item record; relation_name text; emitted integer:=0;
BEGIN
 IF NOT pg_has_role(session_user,'artifact_retention_runtime','MEMBER') OR NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=session_user AND rolcanlogin)
  OR p_owner IS NULL OR octet_length(p_owner) NOT BETWEEN 1 AND 128 OR p_owner ~ '[[:space:][:cntrl:]]'
  OR p_limit IS NULL OR p_lease IS NULL OR p_limit NOT BETWEEN 1 AND 100 OR p_lease NOT BETWEEN 1 AND 600 THEN RAISE EXCEPTION 'Kubernetes stage cleanup claim is invalid'; END IF;
 PERFORM control_plane.kubernetes_file_origins_release(p_limit);
 RETURN QUERY SELECT file.* FROM control_plane.kubernetes_file_source_cleanup_claim(p_owner,p_limit,p_lease) file
  WHERE file.source_kind='KUBERNETES_FILE_SOURCE';
 GET DIAGNOSTICS emitted=ROW_COUNT;
 IF emitted>=p_limit THEN RETURN; END IF;
 FOR candidate IN
  SELECT source.* FROM(
   SELECT 'KUBERNETES_INPUT'::text kind,stage.id,stage.stream_id,stage.created_at FROM control_plane.kubernetes_input_stages stage
    WHERE stage.channel<>'FILE_CONTENT' AND stage.payload_purged_at IS NULL AND stage.cleanup_attempts<3 AND(stage.cleanup_deadline IS NULL OR stage.cleanup_deadline<=clock_timestamp())
     AND(stage.state='ACKNOWLEDGED' OR stage.created_at<=clock_timestamp()-interval '2 minutes')
   UNION ALL SELECT 'KUBERNETES_OUTPUT',stage.id,stage.stream_id,stage.created_at FROM control_plane.kubernetes_output_stages stage
    WHERE stage.payload_purged_at IS NULL AND stage.cleanup_attempts<3 AND(stage.cleanup_deadline IS NULL OR stage.cleanup_deadline<=clock_timestamp())
     AND(stage.state='ACCEPTED' OR stage.created_at<=clock_timestamp()-interval '2 minutes')
   UNION ALL SELECT 'KUBERNETES_PREPARED',prepared.id,capture.stream_id,prepared.created_at FROM control_plane.prepared_content prepared
    JOIN control_plane.kubernetes_output_captures capture ON capture.ref=prepared.source_profile_ref AND capture.organization_id=prepared.organization_id
    WHERE prepared.source_profile='KUBERNETES_OUTPUT' AND prepared.state IN('IN_FLIGHT','UNKNOWN','STAGED','ABANDONED','CLEANING')
     AND prepared.writer_deadline<=clock_timestamp() AND prepared.cleanup_attempts<3 AND prepared.adopted_revision_id IS NULL
     AND(prepared.cleanup_deadline IS NULL OR prepared.cleanup_deadline<=clock_timestamp())
  ) source WHERE control_plane.kubernetes_stage_cleanup_eligible(source.kind,source.id,source.stream_id)
  ORDER BY source.created_at,source.id LIMIT p_limit*4
 LOOP
  EXIT WHEN emitted>=p_limit;
  PERFORM 1 FROM control_plane.kubernetes_execution_streams stream WHERE stream.id=candidate.stream_id FOR UPDATE SKIP LOCKED;
  IF NOT FOUND OR NOT control_plane.kubernetes_stage_cleanup_eligible(candidate.kind,candidate.id,candidate.stream_id) THEN CONTINUE; END IF;
  source_kind:=candidate.kind;ledger_id:=candidate.id;
  IF candidate.kind='KUBERNETES_PREPARED' THEN
   SELECT prepared.* INTO item FROM control_plane.prepared_content prepared WHERE prepared.id=candidate.id
    AND prepared.state IN('IN_FLIGHT','UNKNOWN','STAGED','ABANDONED','CLEANING') AND prepared.adopted_revision_id IS NULL
    AND prepared.writer_deadline<=clock_timestamp() AND prepared.cleanup_attempts<3
    AND(prepared.cleanup_deadline IS NULL OR prepared.cleanup_deadline<=clock_timestamp()) FOR UPDATE SKIP LOCKED;
   IF NOT FOUND OR item.generation=9223372036854775807 OR item.cleanup_total_attempts=9223372036854775807 THEN CONTINUE; END IF;
   UPDATE control_plane.prepared_content prepared SET state='CLEANING',cleanup_owner=p_owner,cleanup_deadline=clock_timestamp()+p_lease*interval '1 second',cleanup_attempts=prepared.cleanup_attempts+1,cleanup_total_attempts=prepared.cleanup_total_attempts+1,generation=prepared.generation+1,updated_at=clock_timestamp() WHERE prepared.id=item.id;
   object_key:=item.object_key;object_version:=item.object_version;object_etag:=item.object_etag;digest:=item.digest;size_bytes:=item.size_bytes;generation:=item.generation+1;
  ELSE
   relation_name:=CASE candidate.kind WHEN 'KUBERNETES_INPUT' THEN 'kubernetes_input_stages' ELSE 'kubernetes_output_stages' END;
   EXECUTE format('SELECT stage.* FROM control_plane.%I stage WHERE id=$1 AND payload_purged_at IS NULL AND cleanup_attempts<3 AND(cleanup_deadline IS NULL OR cleanup_deadline<=clock_timestamp()) FOR UPDATE SKIP LOCKED',relation_name) INTO item USING candidate.id;
   IF item.id IS NULL OR item.cleanup_generation=9223372036854775807 OR item.cleanup_total_attempts=9223372036854775807 THEN CONTINUE; END IF;
   EXECUTE format('UPDATE control_plane.%I SET cleanup_owner=$2,cleanup_deadline=clock_timestamp()+$3*interval ''1 second'',cleanup_attempts=cleanup_attempts+1,cleanup_total_attempts=cleanup_total_attempts+1,cleanup_generation=cleanup_generation+1,cleanup_object_version=coalesce(cleanup_object_version,object_version),cleanup_object_etag=coalesce(cleanup_object_etag,object_etag) WHERE id=$1',relation_name) USING item.id,p_owner,p_lease;
   object_key:=item.object_key;object_version:=coalesce(item.cleanup_object_version,item.object_version,'');object_etag:=coalesce(item.cleanup_object_etag,item.object_etag,'');digest:='sha256:'||item.payload_digest;size_bytes:=item.byte_count;generation:=item.cleanup_generation+1;
  END IF;
  uncertain:=object_version='';emitted:=emitted+1;RETURN NEXT;
 END LOOP;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.kubernetes_stage_cleanup_claim(text,integer,integer) FROM PUBLIC,control_plane_runtime;
GRANT EXECUTE ON FUNCTION control_plane.kubernetes_stage_cleanup_claim(text,integer,integer) TO artifact_retention_runtime;

-- +goose StatementBegin
CREATE FUNCTION control_plane.kubernetes_stage_cleanup_record(p_id uuid,p_kind text,p_owner text,p_generation bigint,p_key text,p_version text,p_etag text,p_digest text,p_size bigint)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE relation_name text; accepted uuid;
BEGIN
 IF p_kind='KUBERNETES_FILE_SOURCE' THEN
  RETURN control_plane.kubernetes_file_source_cleanup_record(p_id,p_owner,p_generation,p_key,p_version,p_etag,p_digest,p_size);
 END IF;
 IF NOT pg_has_role(session_user,'artifact_retention_runtime','MEMBER') OR NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=session_user AND rolcanlogin)
  OR p_kind IS NULL OR p_kind NOT IN('KUBERNETES_INPUT','KUBERNETES_OUTPUT') OR p_version IS NULL OR p_version='' OR lower(p_version)='null' OR p_etag IS NULL OR p_etag='' THEN RETURN false; END IF;
 relation_name:=CASE p_kind WHEN 'KUBERNETES_INPUT' THEN 'kubernetes_input_stages' ELSE 'kubernetes_output_stages' END;
 EXECUTE format('UPDATE control_plane.%I SET cleanup_object_version=$5,cleanup_object_etag=$6 WHERE id=$1 AND cleanup_owner=$2 AND cleanup_generation=$3 AND cleanup_deadline>clock_timestamp() AND payload_purged_at IS NULL AND object_key=$4 AND ''sha256:''||payload_digest=$7 AND byte_count=$8 AND(cleanup_object_version IS NULL OR(cleanup_object_version=$5 AND cleanup_object_etag=$6)) RETURNING id',relation_name)
 INTO accepted USING p_id,p_owner,p_generation,p_key,p_version,p_etag,p_digest,p_size;
 RETURN accepted IS NOT NULL;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.kubernetes_stage_cleanup_record(uuid,text,text,bigint,text,text,text,text,bigint) FROM PUBLIC,control_plane_runtime;
GRANT EXECUTE ON FUNCTION control_plane.kubernetes_stage_cleanup_record(uuid,text,text,bigint,text,text,text,text,bigint) TO artifact_retention_runtime;

-- +goose StatementBegin
CREATE FUNCTION control_plane.kubernetes_stage_cleanup_complete(p_id uuid,p_kind text,p_owner text,p_generation bigint,p_version text,p_etag text,p_success boolean)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE relation_name text; accepted uuid;
BEGIN
 IF p_kind='KUBERNETES_FILE_SOURCE' THEN
  RETURN control_plane.kubernetes_file_source_cleanup_complete(p_id,p_owner,p_generation,p_version,p_etag,p_success);
 END IF;
 IF NOT pg_has_role(session_user,'artifact_retention_runtime','MEMBER') OR NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=session_user AND rolcanlogin)
  OR p_kind IS NULL OR p_kind NOT IN('KUBERNETES_INPUT','KUBERNETES_OUTPUT') OR p_success IS NULL THEN RETURN false; END IF;
 relation_name:=CASE p_kind WHEN 'KUBERNETES_INPUT' THEN 'kubernetes_input_stages' ELSE 'kubernetes_output_stages' END;
 EXECUTE format('UPDATE control_plane.%I SET payload_purged_at=CASE WHEN $7 THEN clock_timestamp() ELSE NULL END,deletion_receipt_digest=CASE WHEN $7 THEN encode(public.digest(convert_to(jsonb_build_object(''stage'',id,''kind'',$2,''generation'',$4,''version'',$5,''etag'',$6)::text,''UTF8''),''sha256''),''hex'') ELSE NULL END,cleanup_owner=NULL,cleanup_deadline=NULL WHERE id=$1 AND cleanup_owner=$3 AND cleanup_generation=$4 AND cleanup_deadline>clock_timestamp() AND payload_purged_at IS NULL AND(NOT $7 OR(cleanup_object_version=$5 AND cleanup_object_etag=$6 AND $5<>'''' AND $6<>'''')) RETURNING id',relation_name)
 INTO accepted USING p_id,p_kind,p_owner,p_generation,p_version,p_etag,p_success;
 RETURN accepted IS NOT NULL;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.kubernetes_stage_cleanup_complete(uuid,text,text,bigint,text,text,boolean) FROM PUBLIC,control_plane_runtime;
GRANT EXECUTE ON FUNCTION control_plane.kubernetes_stage_cleanup_complete(uuid,text,text,bigint,text,text,boolean) TO artifact_retention_runtime;

-- Один cycle выдаёт максимум три claims; total и repair generation только возрастают.
-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_kubernetes_cleanup_counters() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF TG_TABLE_NAME='prepared_content' THEN
  IF NEW.source_profile<>'KUBERNETES_OUTPUT' THEN
   IF NEW.cleanup_total_attempts<>0 OR NEW.cleanup_repair_generation<>0 THEN
    RAISE EXCEPTION 'foreign prepared cleanup counters are invalid' USING ERRCODE='23514'; END IF;
   RETURN NEW;
  END IF;
 END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.cleanup_total_attempts<>0 OR NEW.cleanup_repair_generation<>0 THEN
   RAISE EXCEPTION 'cleanup counters cannot be self-issued' USING ERRCODE='23514'; END IF;
 ELSIF ROW(NEW.cleanup_attempts,NEW.cleanup_total_attempts,NEW.cleanup_repair_generation)
  IS DISTINCT FROM ROW(OLD.cleanup_attempts,OLD.cleanup_total_attempts,OLD.cleanup_repair_generation) THEN
  IF current_user<>'control_plane_owner' OR NOT(
   NEW.cleanup_repair_generation=OLD.cleanup_repair_generation AND OLD.cleanup_total_attempts<9223372036854775807
    AND NEW.cleanup_attempts=OLD.cleanup_attempts+1 AND NEW.cleanup_total_attempts=OLD.cleanup_total_attempts+1
   OR OLD.cleanup_attempts=3 AND NEW.cleanup_attempts=0 AND OLD.cleanup_repair_generation<9223372036854775807
    AND NEW.cleanup_repair_generation=OLD.cleanup_repair_generation+1 AND NEW.cleanup_total_attempts=OLD.cleanup_total_attempts
    AND NEW.cleanup_owner IS NULL AND NEW.cleanup_deadline IS NULL
  ) THEN RAISE EXCEPTION 'cleanup attempt history is immutable' USING ERRCODE='23514'; END IF;
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_kubernetes_cleanup_counters() FROM PUBLIC;
CREATE TRIGGER kubernetes_input_cleanup_counters BEFORE INSERT OR UPDATE ON control_plane.kubernetes_input_stages
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_kubernetes_cleanup_counters();
CREATE TRIGGER kubernetes_output_cleanup_counters BEFORE INSERT OR UPDATE ON control_plane.kubernetes_output_stages
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_kubernetes_cleanup_counters();
CREATE TRIGGER kubernetes_prepared_cleanup_counters BEFORE INSERT OR UPDATE ON control_plane.prepared_content
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_kubernetes_cleanup_counters();

-- Минимальные counters доступны runtime только после domain original-root eligibility.
-- +goose StatementBegin
CREATE FUNCTION control_plane.kubernetes_stage_cleanup_status(p_org uuid,p_stream uuid)
RETURNS TABLE(version bigint,repair_generation bigint,outstanding_count bigint,outstanding_bytes bigint,exhausted_count bigint,eligible_exhausted_count bigint)
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog AS $$
WITH outstanding AS (
 SELECT 'KUBERNETES_INPUT'::text kind,stage.id,stage.byte_count::bigint size,stage.cleanup_attempts,stage.cleanup_deadline
 FROM control_plane.kubernetes_input_stages stage WHERE stage.organization_id=p_org AND stage.stream_id=p_stream AND stage.channel<>'FILE_CONTENT' AND stage.payload_purged_at IS NULL
 UNION ALL SELECT 'KUBERNETES_OUTPUT',stage.id,stage.byte_count,stage.cleanup_attempts,stage.cleanup_deadline
 FROM control_plane.kubernetes_output_stages stage WHERE stage.organization_id=p_org AND stage.stream_id=p_stream AND stage.payload_purged_at IS NULL
 UNION ALL SELECT 'KUBERNETES_PREPARED',prepared.id,prepared.size_bytes,prepared.cleanup_attempts,prepared.cleanup_deadline
 FROM control_plane.prepared_content prepared JOIN control_plane.kubernetes_output_captures capture
  ON capture.ref=prepared.source_profile_ref AND capture.organization_id=prepared.organization_id
 WHERE prepared.organization_id=p_org AND capture.stream_id=p_stream AND prepared.source_profile='KUBERNETES_OUTPUT'
  AND prepared.state NOT IN('CLEANED','ADOPTED') AND prepared.adopted_revision_id IS NULL
)
SELECT stream.cleanup_version,stream.cleanup_repair_generation,
 (SELECT count(*) FROM outstanding),(SELECT coalesce(sum(size),0)::bigint FROM outstanding),
 (SELECT count(*) FROM outstanding WHERE cleanup_attempts=3),
 (SELECT count(*) FROM outstanding WHERE cleanup_attempts=3 AND(cleanup_deadline IS NULL OR cleanup_deadline<=clock_timestamp())
  AND control_plane.kubernetes_stage_cleanup_eligible(kind,id,stream.id))
FROM control_plane.kubernetes_execution_streams stream
WHERE stream.id=p_stream AND stream.organization_id=p_org;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.kubernetes_stage_cleanup_status(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.kubernetes_stage_cleanup_status(uuid,uuid) TO control_plane_runtime;

-- Repair использует existing stream lock/eligibility, не выполняет object effects.
-- +goose StatementBegin
CREATE FUNCTION control_plane.repair_kubernetes_stage_cleanup(p_org uuid,p_stream uuid,p_expected bigint)
RETURNS TABLE(version bigint,repair_generation bigint,repaired_count integer,accepted_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE candidate record; relation_name text; total integer:=0; changed integer;
BEGIN
 IF NOT pg_has_role(session_user,'control_plane_runtime','MEMBER') OR NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=session_user AND rolcanlogin) THEN
  RAISE EXCEPTION 'Kubernetes cleanup repair caller is invalid'; END IF;
 PERFORM 1 FROM control_plane.kubernetes_execution_streams stream WHERE stream.id=p_stream AND stream.organization_id=p_org
  AND stream.state NOT IN('PREPARED','ACTIVE') AND stream.cleanup_version=p_expected
  AND stream.cleanup_version<9223372036854775807 AND stream.cleanup_repair_generation<9223372036854775807 FOR UPDATE;
 IF NOT FOUND THEN RETURN; END IF;
 FOR candidate IN SELECT source.* FROM(
   SELECT 'KUBERNETES_INPUT'::text kind,stage.id,stage.created_at FROM control_plane.kubernetes_input_stages stage
    WHERE stage.organization_id=p_org AND stage.stream_id=p_stream AND stage.channel<>'FILE_CONTENT' AND stage.payload_purged_at IS NULL AND stage.cleanup_attempts=3
     AND(stage.cleanup_deadline IS NULL OR stage.cleanup_deadline<=clock_timestamp())
   UNION ALL SELECT 'KUBERNETES_OUTPUT',stage.id,stage.created_at FROM control_plane.kubernetes_output_stages stage
    WHERE stage.organization_id=p_org AND stage.stream_id=p_stream AND stage.payload_purged_at IS NULL AND stage.cleanup_attempts=3
     AND(stage.cleanup_deadline IS NULL OR stage.cleanup_deadline<=clock_timestamp())
   UNION ALL SELECT 'KUBERNETES_PREPARED',prepared.id,prepared.created_at FROM control_plane.prepared_content prepared
    JOIN control_plane.kubernetes_output_captures capture ON capture.ref=prepared.source_profile_ref AND capture.organization_id=prepared.organization_id
    WHERE prepared.organization_id=p_org AND capture.stream_id=p_stream AND prepared.source_profile='KUBERNETES_OUTPUT'
     AND prepared.state IN('WAITING_OWNER','CLEANING','ABANDONED') AND prepared.cleanup_attempts=3
     AND prepared.writer_deadline<=clock_timestamp() AND prepared.adopted_revision_id IS NULL
     AND(prepared.cleanup_deadline IS NULL OR prepared.cleanup_deadline<=clock_timestamp())
  ) source WHERE control_plane.kubernetes_stage_cleanup_eligible(source.kind,source.id,p_stream)
  ORDER BY source.created_at,source.id LIMIT 100
 LOOP
  IF candidate.kind='KUBERNETES_PREPARED' THEN
   UPDATE control_plane.prepared_content prepared SET state='ABANDONED',cleanup_attempts=0,
    cleanup_repair_generation=prepared.cleanup_repair_generation+1,cleanup_owner=NULL,cleanup_deadline=NULL,
    generation=prepared.generation+1,updated_at=clock_timestamp()
   WHERE prepared.id=candidate.id AND prepared.generation<9223372036854775807 AND prepared.cleanup_repair_generation<9223372036854775807;
  ELSE
   relation_name:=CASE candidate.kind WHEN 'KUBERNETES_INPUT' THEN 'kubernetes_input_stages' ELSE 'kubernetes_output_stages' END;
   EXECUTE format('UPDATE control_plane.%I SET cleanup_attempts=0,cleanup_repair_generation=cleanup_repair_generation+1,cleanup_owner=NULL,cleanup_deadline=NULL,cleanup_generation=cleanup_generation+1 WHERE id=$1 AND cleanup_generation<9223372036854775807 AND cleanup_repair_generation<9223372036854775807',relation_name) USING candidate.id;
  END IF;
  GET DIAGNOSTICS changed=ROW_COUNT;
  total:=total+changed;
 END LOOP;
 IF total=0 THEN RETURN; END IF;
 RETURN QUERY UPDATE control_plane.kubernetes_execution_streams stream SET cleanup_version=stream.cleanup_version+1,
  cleanup_repair_generation=stream.cleanup_repair_generation+1 WHERE stream.id=p_stream AND stream.organization_id=p_org
  RETURNING stream.cleanup_version,stream.cleanup_repair_generation,total,clock_timestamp();
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.repair_kubernetes_stage_cleanup(uuid,uuid,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.repair_kubernetes_stage_cleanup(uuid,uuid,bigint) TO control_plane_runtime;

CREATE TABLE control_plane.kubernetes_execution_completions (
 effect_id uuid PRIMARY KEY,
 organization_id uuid NOT NULL REFERENCES control_plane.organizations(id) DEFERRABLE INITIALLY IMMEDIATE,
 outcome text NOT NULL CHECK(outcome IN('SUCCEEDED','FAILED','UNKNOWN')),
 result_json text NOT NULL CHECK(jsonb_typeof(result_json::jsonb)='object' AND octet_length(result_json)<=65536),
 result_digest text NOT NULL CHECK(result_digest ~ '^[a-f0-9]{64}$'),
 submission_digest text NOT NULL CHECK(submission_digest ~ '^[a-f0-9]{64}$'),
 reason_code text NOT NULL CHECK(reason_code='' OR reason_code ~ '^[A-Z][A-Z0-9_]{1,95}$'),
 applied boolean NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 FOREIGN KEY(effect_id,organization_id) REFERENCES control_plane.kubernetes_execution_effects(id,organization_id) DEFERRABLE INITIALLY IMMEDIATE,
 CHECK(result_digest=encode(public.digest(convert_to(result_json,'UTF8'),'sha256'),'hex')),
 CHECK(applied OR result_json='{}'),
 CHECK(NOT applied OR submission_digest=result_digest)
);
-- PF connection state расширяет исходный stream/INPUT/OUTPUT graph; отдельной lease либо queue нет.
-- Предел16 относится лишь к одновременно активным incarnations, не к последовательным открытиям.
ALTER TABLE control_plane.kubernetes_execution_streams ADD COLUMN pf_connection_generation bigint NOT NULL DEFAULT 0 CHECK(pf_connection_generation>=0);
CREATE TABLE control_plane.kubernetes_port_forward_connections (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 ref text NOT NULL UNIQUE CHECK(ref ~ '^kpf_[A-Za-z0-9_-]{24}$'),
 organization_id uuid NOT NULL REFERENCES control_plane.organizations(id) DEFERRABLE INITIALLY IMMEDIATE,
 stream_id uuid NOT NULL,
 actor_id uuid NOT NULL REFERENCES control_plane.subjects(id) DEFERRABLE INITIALLY IMMEDIATE,
 generation bigint NOT NULL CHECK(generation>0),
 slot smallint NOT NULL CHECK(slot BETWEEN 0 AND 15),
 open_key text NOT NULL CHECK(octet_length(open_key) BETWEEN 8 AND 128),
 open_intent_digest text NOT NULL CHECK(open_intent_digest ~ '^[a-f0-9]{64}$'),
 open_expected_version bigint NOT NULL CHECK(open_expected_version>0),
 open_stage_id uuid NOT NULL UNIQUE REFERENCES control_plane.kubernetes_input_stages(id) DEFERRABLE INITIALLY IMMEDIATE,
 close_stage_id uuid UNIQUE REFERENCES control_plane.kubernetes_input_stages(id) DEFERRABLE INITIALLY IMMEDIATE,
 state text NOT NULL CHECK(state IN('PREPARED','OPENING','OPEN','CLOSED','UNKNOWN','CANCELLED','EXPIRED')),
 reason_code text NOT NULL DEFAULT '' CHECK(reason_code='' OR reason_code ~ '^[A-Z][A-Z0-9_]{1,95}$'),
 local_write_closed_at timestamptz,
 read_end_observed_at timestamptz,
 native_joined_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 closed_at timestamptz,
 UNIQUE(id,organization_id),UNIQUE(id,stream_id,organization_id),
 UNIQUE(ref,generation,stream_id,organization_id),
 UNIQUE(stream_id,generation),UNIQUE(organization_id,stream_id,actor_id,open_key),
 FOREIGN KEY(stream_id,organization_id) REFERENCES control_plane.kubernetes_execution_streams(id,organization_id) DEFERRABLE INITIALLY IMMEDIATE,
 CHECK((state IN('PREPARED','OPENING','OPEN'))=(closed_at IS NULL)),
 CHECK(native_joined_at IS NULL OR closed_at IS NOT NULL),
 CHECK(close_stage_id IS NULL OR closed_at IS NOT NULL)
);
CREATE INDEX kubernetes_port_forward_connection_org ON control_plane.kubernetes_port_forward_connections(organization_id);
CREATE INDEX kubernetes_port_forward_active ON control_plane.kubernetes_port_forward_connections(organization_id,stream_id)
 WHERE state IN('PREPARED','OPENING','OPEN');
ALTER TABLE control_plane.kubernetes_capture_losses ADD CONSTRAINT kubernetes_capture_loss_port_forward_source
 FOREIGN KEY(source_ref,source_generation,stream_id,organization_id) REFERENCES control_plane.kubernetes_port_forward_connections(ref,generation,stream_id,organization_id) DEFERRABLE INITIALLY IMMEDIATE;
CREATE TABLE control_plane.kubernetes_port_forward_frames (
 organization_id uuid NOT NULL REFERENCES control_plane.organizations(id) DEFERRABLE INITIALLY IMMEDIATE,
 stream_id uuid NOT NULL,
 direction text NOT NULL CHECK(direction IN('INPUT','OUTPUT')),
 sequence bigint NOT NULL CHECK(sequence>0),
 connection_id uuid NOT NULL,
 action text NOT NULL CHECK(action IN('OPEN','DATA','HALF_CLOSE','CLOSE')),
 native_ordinal bigint NOT NULL CHECK(native_ordinal>=0),
 payload_digest text NOT NULL CHECK(payload_digest ~ '^[a-f0-9]{64}$'),
 byte_count integer NOT NULL CHECK(byte_count BETWEEN 51 AND 32768),
 input_stage_id uuid UNIQUE REFERENCES control_plane.kubernetes_input_stages(id) DEFERRABLE INITIALLY IMMEDIATE,
 output_direction text GENERATED ALWAYS AS (CASE WHEN direction='OUTPUT' THEN 'OUTPUT'::text ELSE NULL::text END) STORED,
 PRIMARY KEY(stream_id,direction,sequence),
 FOREIGN KEY(stream_id,organization_id) REFERENCES control_plane.kubernetes_execution_streams(id,organization_id) DEFERRABLE INITIALLY IMMEDIATE,
 FOREIGN KEY(connection_id,stream_id,organization_id) REFERENCES control_plane.kubernetes_port_forward_connections(id,stream_id,organization_id) DEFERRABLE INITIALLY IMMEDIATE,
 FOREIGN KEY(stream_id,output_direction,sequence) REFERENCES control_plane.kubernetes_execution_frames(stream_id,direction,sequence) DEFERRABLE INITIALLY IMMEDIATE,
 CHECK((direction='INPUT')=(input_stage_id IS NOT NULL)),
 CHECK((direction='INPUT')=(native_ordinal=0)),
 CHECK(direction='INPUT' OR action<>'OPEN'),
 CHECK((action='DATA' AND byte_count>51) OR(action<>'DATA' AND byte_count=51))
);
CREATE UNIQUE INDEX kubernetes_port_forward_output_event ON control_plane.kubernetes_port_forward_frames(connection_id,native_ordinal) WHERE direction='OUTPUT';
-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_kubernetes_port_forward_generation() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF NEW.pf_connection_generation IS DISTINCT FROM OLD.pf_connection_generation
  AND(NEW.state<>'ACTIVE' OR OLD.pf_connection_generation=9223372036854775807
   OR NEW.pf_connection_generation<>OLD.pf_connection_generation+1) THEN
  RAISE EXCEPTION 'invalid Kubernetes port-forward generation' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_kubernetes_port_forward_generation() FROM PUBLIC;
CREATE TRIGGER kubernetes_port_forward_generation_guard BEFORE UPDATE OF pf_connection_generation ON control_plane.kubernetes_execution_streams
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_kubernetes_port_forward_generation();
-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_kubernetes_port_forward_connection() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF TG_OP='DELETE' THEN
  IF NOT control_plane.project_purge_authorized() THEN RAISE EXCEPTION 'immutable Kubernetes port-forward connection' USING ERRCODE='23514'; END IF;
  RETURN OLD;
 END IF;
 IF TG_OP='INSERT' THEN
  PERFORM stream.id FROM control_plane.kubernetes_execution_streams stream
   WHERE stream.id=NEW.stream_id AND stream.organization_id=NEW.organization_id FOR UPDATE;
  IF NEW.state<>'PREPARED' OR NEW.close_stage_id IS NOT NULL OR NEW.closed_at IS NOT NULL
   OR NEW.local_write_closed_at IS NOT NULL OR NEW.read_end_observed_at IS NOT NULL OR NEW.native_joined_at IS NOT NULL
   OR(SELECT count(*) FROM control_plane.kubernetes_port_forward_connections active
    WHERE active.stream_id=NEW.stream_id AND active.organization_id=NEW.organization_id AND active.state IN('PREPARED','OPENING','OPEN'))>=16
   OR NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_execution_streams stream
    JOIN control_plane.kubernetes_execution_effects effect ON effect.id=stream.effect_id AND effect.organization_id=stream.organization_id
    JOIN control_plane.kubernetes_execution_children child ON child.id=effect.child_id AND child.organization_id=effect.organization_id
    JOIN control_plane.kubernetes_input_stages stage ON stage.id=NEW.open_stage_id AND stage.stream_id=stream.id AND stage.organization_id=stream.organization_id
    WHERE stream.id=NEW.stream_id AND stream.organization_id=NEW.organization_id AND stream.state='ACTIVE'
     AND stream.pf_connection_generation=NEW.generation AND child.operation='kubernetes.port-forward'
     AND stage.actor_id=NEW.actor_id AND stage.channel='PORT_FORWARD' AND stage.byte_count=51 AND NOT stage.end_of_input
     AND stage.submission_key=NEW.open_key AND stage.state='ATTEMPTING') THEN
   RAISE EXCEPTION 'invalid Kubernetes port-forward connection' USING ERRCODE='23514';
  END IF;
 ELSE
  IF ROW(NEW.id,NEW.ref,NEW.organization_id,NEW.stream_id,NEW.actor_id,NEW.generation,NEW.slot,NEW.open_key,NEW.open_intent_digest,NEW.open_expected_version,NEW.open_stage_id,NEW.created_at)
   IS DISTINCT FROM ROW(OLD.id,OLD.ref,OLD.organization_id,OLD.stream_id,OLD.actor_id,OLD.generation,OLD.slot,OLD.open_key,OLD.open_intent_digest,OLD.open_expected_version,OLD.open_stage_id,OLD.created_at)
   OR OLD.close_stage_id IS NOT NULL AND NEW.close_stage_id IS DISTINCT FROM OLD.close_stage_id
   OR OLD.closed_at IS NOT NULL AND NEW.closed_at IS DISTINCT FROM OLD.closed_at
   OR OLD.local_write_closed_at IS NOT NULL AND NEW.local_write_closed_at IS DISTINCT FROM OLD.local_write_closed_at
   OR OLD.read_end_observed_at IS NOT NULL AND NEW.read_end_observed_at IS DISTINCT FROM OLD.read_end_observed_at
   OR OLD.native_joined_at IS NOT NULL AND NEW.native_joined_at IS DISTINCT FROM OLD.native_joined_at
   OR NOT(NEW.state=OLD.state OR OLD.state='PREPARED' AND NEW.state IN('OPENING','CLOSED','UNKNOWN','CANCELLED','EXPIRED')
    OR OLD.state='OPENING' AND NEW.state IN('OPEN','CLOSED','UNKNOWN','CANCELLED','EXPIRED')
    OR OLD.state='OPEN' AND NEW.state IN('CLOSED','UNKNOWN','CANCELLED','EXPIRED')) THEN
   RAISE EXCEPTION 'immutable Kubernetes port-forward connection' USING ERRCODE='23514';
  END IF;
  IF NEW.state IN('OPENING','OPEN') AND NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_input_stages stage
   WHERE stage.id=NEW.open_stage_id AND stage.organization_id=NEW.organization_id AND stage.stream_id=NEW.stream_id
    AND stage.native_admitted_at IS NOT NULL AND(NEW.state<>'OPEN' OR stage.acknowledged_at IS NOT NULL)) THEN
   RAISE EXCEPTION 'Kubernetes port-forward admission is unavailable' USING ERRCODE='23514';
  END IF;
  IF NEW.close_stage_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_input_stages stage
   WHERE stage.id=NEW.close_stage_id AND stage.organization_id=NEW.organization_id AND stage.stream_id=NEW.stream_id
    AND stage.channel='PORT_FORWARD' AND NOT stage.end_of_input AND stage.byte_count=51) THEN
   RAISE EXCEPTION 'Kubernetes port-forward closure is unavailable' USING ERRCODE='23514';
  END IF;
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_kubernetes_port_forward_connection() FROM PUBLIC;
CREATE TRIGGER kubernetes_port_forward_connection_guard BEFORE INSERT OR UPDATE OR DELETE ON control_plane.kubernetes_port_forward_connections
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_kubernetes_port_forward_connection();
-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_kubernetes_port_forward_frame() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF TG_OP='DELETE' THEN
  IF NOT control_plane.project_purge_authorized() THEN RAISE EXCEPTION 'immutable Kubernetes port-forward frame' USING ERRCODE='23514'; END IF;
  RETURN OLD;
 END IF;
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'immutable Kubernetes port-forward frame' USING ERRCODE='23514'; END IF;
 IF NEW.direction='INPUT' THEN
  IF NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_input_stages stage
   WHERE stage.id=NEW.input_stage_id AND stage.organization_id=NEW.organization_id AND stage.stream_id=NEW.stream_id
    AND stage.sequence=NEW.sequence AND stage.channel='PORT_FORWARD' AND NOT stage.end_of_input
    AND stage.payload_digest=NEW.payload_digest AND stage.byte_count=NEW.byte_count) THEN
   RAISE EXCEPTION 'invalid Kubernetes port-forward input binding' USING ERRCODE='23514';
  END IF;
 ELSE
  IF NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_execution_frames frame
   WHERE frame.stream_id=NEW.stream_id AND frame.organization_id=NEW.organization_id AND frame.direction='OUTPUT'
    AND frame.sequence=NEW.sequence AND frame.channel='PORT_FORWARD' AND NOT frame.end_of_input
    AND frame.payload_digest=NEW.payload_digest AND frame.byte_count=NEW.byte_count) THEN
   RAISE EXCEPTION 'invalid Kubernetes port-forward output binding' USING ERRCODE='23514';
  END IF;
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_kubernetes_port_forward_frame() FROM PUBLIC;
CREATE TRIGGER kubernetes_port_forward_frame_guard BEFORE INSERT OR UPDATE OR DELETE ON control_plane.kubernetes_port_forward_frames
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_kubernetes_port_forward_frame();
-- +goose StatementBegin
CREATE FUNCTION control_plane.advance_kubernetes_port_forward_version() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 UPDATE control_plane.kubernetes_execution_streams SET input_version=input_version+1
 WHERE id=NEW.stream_id AND organization_id=NEW.organization_id;
 RETURN NULL;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.advance_kubernetes_port_forward_version() FROM PUBLIC;
CREATE TRIGGER kubernetes_port_forward_connection_version AFTER INSERT OR UPDATE ON control_plane.kubernetes_port_forward_connections
 FOR EACH ROW EXECUTE FUNCTION control_plane.advance_kubernetes_port_forward_version();
-- +goose StatementBegin
CREATE FUNCTION control_plane.close_kubernetes_port_forward_connections() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF NEW.state NOT IN('PREPARED','ACTIVE') AND NEW.state IS DISTINCT FROM OLD.state THEN
  UPDATE control_plane.kubernetes_port_forward_connections SET
   state=CASE WHEN state='OPENING' THEN 'UNKNOWN' WHEN NEW.state='EXPIRED' THEN 'EXPIRED' ELSE 'CANCELLED' END,
   reason_code=CASE WHEN state='OPENING' THEN 'KUBERNETES_TRANSPORT_UNKNOWN' WHEN NEW.state='EXPIRED' THEN 'KUBERNETES_LEASE_EXPIRED' ELSE 'KUBERNETES_STREAM_CANCELED' END,
   closed_at=clock_timestamp()
  WHERE stream_id=NEW.id AND organization_id=NEW.organization_id AND state IN('PREPARED','OPENING','OPEN');
 END IF;
 RETURN NULL;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.close_kubernetes_port_forward_connections() FROM PUBLIC;
CREATE TRIGGER kubernetes_port_forward_stream_closure AFTER UPDATE OF state ON control_plane.kubernetes_execution_streams
 FOR EACH ROW EXECUTE FUNCTION control_plane.close_kubernetes_port_forward_connections();
REVOKE ALL ON control_plane.kubernetes_port_forward_connections,control_plane.kubernetes_port_forward_frames FROM control_plane_runtime;
GRANT SELECT,INSERT,UPDATE ON control_plane.kubernetes_port_forward_connections TO control_plane_runtime;
GRANT SELECT,INSERT ON control_plane.kubernetes_port_forward_frames TO control_plane_runtime;
ALTER TABLE control_plane.owner_gates DROP CONSTRAINT owner_gates_integration_invocation_id_key;
ALTER TABLE control_plane.owner_gates ADD COLUMN kubernetes_child_id uuid UNIQUE REFERENCES control_plane.kubernetes_execution_children(id) DEFERRABLE INITIALLY IMMEDIATE;
ALTER TABLE control_plane.owner_gates ADD CONSTRAINT owner_gates_kubernetes_child_invocation CHECK(kubernetes_child_id IS NULL OR integration_invocation_id IS NOT NULL);
ALTER TABLE control_plane.owner_gates ADD CONSTRAINT owner_gates_kubernetes_child_tenant
 FOREIGN KEY(kubernetes_child_id,organization_id) REFERENCES control_plane.kubernetes_execution_children(id,organization_id) DEFERRABLE INITIALLY IMMEDIATE;
CREATE UNIQUE INDEX owner_gates_ordinary_invocation_unique ON control_plane.owner_gates(integration_invocation_id) WHERE kubernetes_child_id IS NULL;
CREATE TRIGGER kubernetes_execution_plan_immutable BEFORE UPDATE OR DELETE ON control_plane.kubernetes_execution_plans
 FOR EACH ROW WHEN(NOT control_plane.project_purge_authorized()) EXECUTE FUNCTION control_plane.reject_integration_immutable_update();
CREATE TRIGGER kubernetes_execution_child_immutable BEFORE UPDATE OR DELETE ON control_plane.kubernetes_execution_children
 FOR EACH ROW WHEN(NOT control_plane.project_purge_authorized()) EXECUTE FUNCTION control_plane.reject_integration_immutable_update();
CREATE TRIGGER kubernetes_execution_completion_immutable BEFORE UPDATE OR DELETE ON control_plane.kubernetes_execution_completions
 FOR EACH ROW WHEN(NOT control_plane.project_purge_authorized()) EXECUTE FUNCTION control_plane.reject_integration_immutable_update();
REVOKE ALL ON control_plane.kubernetes_execution_plans,control_plane.kubernetes_execution_children,
 control_plane.kubernetes_execution_effects,control_plane.kubernetes_execution_streams,
 control_plane.kubernetes_execution_frames,control_plane.kubernetes_execution_completions FROM control_plane_runtime;
GRANT SELECT,INSERT ON control_plane.kubernetes_execution_plans,control_plane.kubernetes_execution_children,
 control_plane.kubernetes_execution_completions TO control_plane_runtime;
GRANT SELECT,INSERT,UPDATE ON control_plane.kubernetes_execution_effects,control_plane.kubernetes_execution_streams,
 control_plane.kubernetes_execution_frames TO control_plane_runtime;

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_kubernetes_child_gate() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF NEW.kubernetes_child_id IS NULL THEN RETURN NEW; END IF;
 IF NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_execution_children child
  JOIN control_plane.kubernetes_execution_plans plan ON plan.id=child.plan_id AND plan.organization_id=child.organization_id
  JOIN control_plane.integration_invocations invocation ON invocation.id=plan.invocation_id AND invocation.organization_id=plan.organization_id
  JOIN control_plane.kubernetes_invocation_identity_bindings pin ON pin.invocation_id=invocation.id AND pin.organization_id=invocation.organization_id
  JOIN control_plane.run_nodes gate_node ON gate_node.id=NEW.node_id AND gate_node.organization_id=NEW.organization_id
  JOIN control_plane.runs root ON root.id=pin.root_run_id AND root.organization_id=pin.organization_id
  WHERE child.id=NEW.kubernetes_child_id AND child.organization_id=NEW.organization_id
   AND child.approval_policy='HUMAN_EACH_EFFECT' AND invocation.id=NEW.integration_invocation_id
   AND root.id=NEW.root_run_id AND root.project_id IS NOT DISTINCT FROM NEW.project_id
   AND gate_node.parent_node_id=invocation.node_id AND gate_node.type='HUMAN_GATE') THEN
  RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='managed Kubernetes gate lineage mismatch';
 END IF;
 IF TG_OP='UPDATE' AND (NEW.kubernetes_child_id IS DISTINCT FROM OLD.kubernetes_child_id
  OR NEW.integration_invocation_id IS DISTINCT FROM OLD.integration_invocation_id OR NEW.node_id<>OLD.node_id
  OR NEW.organization_id<>OLD.organization_id OR NEW.root_run_id<>OLD.root_run_id) THEN
  RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='managed Kubernetes gate lineage is immutable';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_kubernetes_child_gate() FROM PUBLIC;
CREATE TRIGGER kubernetes_child_gate_lineage BEFORE INSERT OR UPDATE ON control_plane.owner_gates
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_kubernetes_child_gate();

-- Terminal исходного invocation атомарно закрывает только его KUBE grants.
-- Immutable plans/children/completions остаются историей, поздний ACK их не оживляет.
-- +goose StatementBegin
CREATE FUNCTION control_plane.close_kubernetes_execution_dependencies() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE terminal_stream text; actor uuid;
BEGIN
 IF NEW.resource_kind<>'KUBERNETES_TARGET' OR NEW.state NOT IN('SUCCEEDED','FAILED','REJECTED','CANCELLED','UNKNOWN_OUTCOME')
  OR NEW.state IS NOT DISTINCT FROM OLD.state THEN RETURN NEW; END IF;
 SELECT root_actor_id INTO actor FROM control_plane.kubernetes_invocation_identity_bindings
  WHERE invocation_id=NEW.id AND organization_id=NEW.organization_id;
 terminal_stream:=CASE WHEN NEW.state IN('REJECTED','CANCELLED') THEN 'CANCELED' ELSE 'LOST' END;
 -- Все consumers берут stream/effect до frame: late ACK не инвертирует terminal lock order.
 PERFORM stream.id FROM control_plane.kubernetes_execution_streams stream
 JOIN control_plane.kubernetes_execution_effects effect ON effect.id=stream.effect_id AND effect.organization_id=stream.organization_id
 JOIN control_plane.kubernetes_execution_children child ON child.id=effect.child_id AND child.organization_id=effect.organization_id
 JOIN control_plane.kubernetes_execution_plans plan ON plan.id=child.plan_id AND plan.organization_id=child.organization_id
 WHERE plan.invocation_id=NEW.id AND plan.organization_id=NEW.organization_id
 ORDER BY stream.id FOR UPDATE OF stream,effect;
 UPDATE control_plane.kubernetes_execution_frames frame
 SET grant_expires_at=least(frame.grant_expires_at,clock_timestamp())
 FROM control_plane.kubernetes_execution_streams stream,control_plane.kubernetes_execution_effects effect,
  control_plane.kubernetes_execution_children child,control_plane.kubernetes_execution_plans plan
 WHERE frame.stream_id=stream.id AND frame.organization_id=stream.organization_id
  AND stream.effect_id=effect.id AND stream.organization_id=effect.organization_id
  AND effect.child_id=child.id AND effect.organization_id=child.organization_id
  AND child.plan_id=plan.id AND child.organization_id=plan.organization_id
  AND plan.invocation_id=NEW.id AND plan.organization_id=NEW.organization_id;
 UPDATE control_plane.kubernetes_execution_streams stream SET state=terminal_stream,closed_at=clock_timestamp()
 FROM control_plane.kubernetes_execution_effects effect,control_plane.kubernetes_execution_children child,
  control_plane.kubernetes_execution_plans plan
 WHERE stream.effect_id=effect.id AND stream.organization_id=effect.organization_id
  AND effect.child_id=child.id AND effect.organization_id=child.organization_id
  AND child.plan_id=plan.id AND child.organization_id=plan.organization_id
  AND plan.invocation_id=NEW.id AND plan.organization_id=NEW.organization_id AND stream.state IN('PREPARED','ACTIVE');
 UPDATE control_plane.kubernetes_execution_effects effect SET state='UNKNOWN',closed_at=clock_timestamp()
 FROM control_plane.kubernetes_execution_children child,control_plane.kubernetes_execution_plans plan
 WHERE effect.child_id=child.id AND effect.organization_id=child.organization_id
  AND child.plan_id=plan.id AND child.organization_id=plan.organization_id
  AND plan.invocation_id=NEW.id AND plan.organization_id=NEW.organization_id AND effect.state='ATTEMPTING';
 -- Закрываем как child Gates, так и исходный HUMAN_SCOPED Gate до claim.
 -- Exact invocation FK связывает все варианты с тем же owner graph.
 UPDATE control_plane.run_nodes node SET state='CANCELLED',finished_at=clock_timestamp(),version=node.version+1
 FROM control_plane.owner_gates gate
 WHERE node.id=gate.node_id AND node.organization_id=gate.organization_id
  AND gate.integration_invocation_id=NEW.id AND gate.organization_id=NEW.organization_id
  AND gate.state='OPEN' AND node.state='WAITING';
 UPDATE control_plane.owner_gates gate SET state='CANCELLED',decision='CANCEL',decision_comment='',resolved_by=actor,
  resolved_at=clock_timestamp(),version=gate.version+1
 WHERE gate.integration_invocation_id=NEW.id AND gate.organization_id=NEW.organization_id AND gate.state='OPEN';
 -- Завершённая отправка и её receipt не меняются; CLAIMED сохраняет uncertainty.
 UPDATE control_plane.interaction_deliveries delivery
 SET state=CASE WHEN delivery.state='CLAIMED' THEN 'UNKNOWN_OUTCOME' ELSE 'CANCELLED' END,
  safe_error_code=CASE WHEN delivery.state='CLAIMED' THEN 'INTERACTION_OUTCOME_UNKNOWN' ELSE delivery.safe_error_code END,
  lease_ref=NULL,fence_digest=NULL,workload_instance=NULL,lease_expires_at=NULL,
  version=delivery.version+1,updated_at=clock_timestamp(),completed_at=clock_timestamp()
 FROM control_plane.owner_gates gate
 WHERE delivery.gate_id=gate.id AND delivery.organization_id=gate.organization_id
  AND gate.integration_invocation_id=NEW.id AND gate.organization_id=NEW.organization_id
  AND delivery.state IN('DUE','FAILED','CLAIMED');
 -- Как в canonical Gate resolver, последний закрытый KUBE Gate снимает только
 -- human wait. Другие OPEN Gates и terminal Run не затрагиваются.
 -- Отдельный statement получает root mutex до fresh OPEN predicate. Один UPDATE
 -- с NOT EXISTS может сохранить snapshot до ожидания конкурентного Gate opener.
 PERFORM root.id FROM control_plane.runs root
 JOIN control_plane.runs execution ON execution.root_run_id=root.id AND execution.organization_id=root.organization_id
 WHERE execution.id=NEW.run_id AND execution.organization_id=NEW.organization_id
  AND root.organization_id=NEW.organization_id
  AND EXISTS(SELECT 1 FROM control_plane.owner_gates gate WHERE gate.integration_invocation_id=NEW.id
   AND gate.organization_id=NEW.organization_id)
 FOR UPDATE OF root;
 UPDATE control_plane.runs root SET state='RUNNING',version=root.version+1,updated_at=clock_timestamp()
 FROM control_plane.runs execution
 WHERE execution.id=NEW.run_id AND execution.organization_id=NEW.organization_id
  AND root.id=execution.root_run_id AND root.organization_id=NEW.organization_id
  AND root.state='WAITING_HUMAN'
  AND EXISTS(SELECT 1 FROM control_plane.owner_gates gate WHERE gate.integration_invocation_id=NEW.id
   AND gate.organization_id=NEW.organization_id)
  AND NOT EXISTS(SELECT 1 FROM control_plane.owner_gates gate WHERE gate.root_run_id=root.id
   AND gate.organization_id=root.organization_id AND gate.state='OPEN');
 RETURN NEW;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.close_kubernetes_execution_dependencies() FROM PUBLIC;
CREATE TRIGGER kubernetes_execution_terminal_closure AFTER UPDATE OF state ON control_plane.integration_invocations
 FOR EACH ROW EXECUTE FUNCTION control_plane.close_kubernetes_execution_dependencies();

-- Полный original binding остаётся проверяемым после очистки live lease.
-- Значение назначается только при новом server-owned claim, без backfill истории.
ALTER TABLE control_plane.integration_invocations ADD COLUMN kubernetes_claimed_instance text
 CHECK(kubernetes_claimed_instance IS NULL OR octet_length(kubernetes_claimed_instance) BETWEEN 1 AND 256);
-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_kubernetes_claimed_instance() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.kubernetes_claimed_instance IS NOT NULL THEN RAISE EXCEPTION 'Kubernetes original claim requires owner admission'; END IF;
  RETURN NEW;
 END IF;
 IF NEW.resource_kind='KUBERNETES_TARGET' AND NEW.generation>OLD.generation THEN
  IF NEW.state<>'RUNNING' OR NEW.claimed_workload<>'integration-gateway'
   OR NEW.claimed_lease_ref IS DISTINCT FROM NEW.lease_ref
   OR NEW.claimed_fence_digest IS DISTINCT FROM NEW.effect_fence_digest
   OR NEW.workload_instance IS NULL OR NEW.workload_instance='' THEN
   RAISE EXCEPTION 'invalid Kubernetes original claim';
  END IF;
  NEW.kubernetes_claimed_instance:=NEW.workload_instance;
 ELSIF NEW.kubernetes_claimed_instance IS DISTINCT FROM OLD.kubernetes_claimed_instance THEN
  RAISE EXCEPTION 'Kubernetes original claim instance is immutable';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_kubernetes_claimed_instance() FROM PUBLIC;
CREATE TRIGGER kubernetes_claimed_instance_guard BEFORE INSERT OR UPDATE ON control_plane.integration_invocations
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_kubernetes_claimed_instance();

-- Один canonical predicate обслуживает current generic reads и owner-pinned FILE history.
-- +goose StatementBegin
CREATE FUNCTION control_plane.runtime_file_source_visible_revision(
    p_tenant uuid,p_actor uuid,p_project uuid,p_agent uuid,p_artifact uuid,p_purpose text,p_source_revision text,p_node uuid,p_owner_entry uuid
) RETURNS boolean LANGUAGE sql STABLE SECURITY INVOKER
SET search_path=pg_catalog,control_plane AS $$
    SELECT EXISTS (
        SELECT 1 FROM control_plane.catalog_access_targets file_target
        JOIN control_plane.artifacts current_file ON current_file.id=file_target.id AND current_file.organization_id=p_tenant
          AND current_file.lifecycle_state='ACTIVE' AND current_file.scan_state IN ('CLEAN','NOT_SCANNED')
        LEFT JOIN control_plane.runtime_file_catalog_entries pinned ON pinned.id=p_owner_entry
          AND pinned.artifact_id=file_target.id AND pinned.purpose=p_purpose AND pinned.source_revision_ref=p_source_revision
        LEFT JOIN control_plane.runtime_file_catalogs original_catalog ON original_catalog.id=pinned.catalog_id
          AND original_catalog.organization_id=p_tenant AND original_catalog.actor_id=p_actor
          AND original_catalog.project_id=p_project AND original_catalog.agent_id=p_agent AND original_catalog.node_id=p_node
          AND original_catalog.frozen AND p_purpose=ANY(original_catalog.purposes)
        JOIN control_plane.artifact_history file ON file.id=file_target.id AND file.organization_id=p_tenant
          AND ((p_owner_entry IS NULL AND file.revision_id=current_file.revision_id)
           OR(p_owner_entry IS NOT NULL AND original_catalog.id IS NOT NULL AND p_purpose IN('PROJECT','WORKSPACE_INPUT','RUN_RESULT')
            AND file.revision=pinned.artifact_revision AND file.digest=pinned.artifact_digest
            AND file.size_bytes=pinned.size_bytes AND file.media_type=pinned.media_type AND file.source=pinned.source
            AND pinned.entry_digest=control_plane.runtime_file_entry_digest(pinned)))
        JOIN control_plane.artifact_revision_content content ON content.revision_id=file.revision_id
          AND content.digest=file.digest AND content.size_bytes=file.size_bytes
        JOIN control_plane.catalog_access_targets project_target
          ON project_target.organization_id=p_tenant AND project_target.kind='PROJECT' AND project_target.id=p_project
        LEFT JOIN control_plane.catalog_access_targets agent_target
          ON agent_target.organization_id=p_tenant AND agent_target.kind='AGENT' AND agent_target.id=p_agent
          AND agent_target.project_id=p_project
        JOIN control_plane.agents current_agent ON current_agent.id=p_agent AND current_agent.organization_id=p_tenant
        WHERE file_target.organization_id=p_tenant AND file_target.kind='ARTIFACT' AND file.id=p_artifact
          AND control_plane.kubernetes_capture_runtime_visible(p_tenant,p_actor,p_artifact,p_node,p_purpose)
          AND (file.project_id=p_project OR (p_purpose='WORKSPACE_INPUT' AND file.project_id IS NULL
              AND file.created_by=p_actor AND current_agent.system_key='system-assistant'))
          AND file.lifecycle_state='ACTIVE' AND file.scan_state IN ('CLEAN','NOT_SCANNED')
          AND control_plane.catalog_resource_visible(p_tenant,p_actor,'project.view','PROJECT',project_target.id,
            project_target.project_id,project_target.owner_id,project_target.related_ids,statement_timestamp(),false)
          AND (control_plane.catalog_resource_visible(p_tenant,p_actor,'agent.view','AGENT',agent_target.id,
            agent_target.project_id,agent_target.owner_id,agent_target.related_ids,statement_timestamp(),false)
            OR (current_agent.system_key='system-assistant' AND current_agent.project_id IS NULL AND current_agent.state<>'ARCHIVED'))
          AND control_plane.catalog_resource_visible(p_tenant,p_actor,'artifact.view','ARTIFACT',file_target.id,
            file_target.project_id,file_target.owner_id,file_target.related_ids,statement_timestamp())
          AND control_plane.catalog_resource_visible(p_tenant,p_actor,'artifact.download','ARTIFACT',file_target.id,
            file_target.project_id,file_target.owner_id,file_target.related_ids,statement_timestamp())
          AND CASE p_purpose
            WHEN 'PROJECT' THEN file.run_id IS NULL AND 'platform.artifact.manage'=ANY(current_agent.capabilities)
            WHEN 'WORKSPACE_INPUT' THEN 'platform.artifact.manage'=ANY(current_agent.capabilities)
            WHEN 'RUN_RESULT' THEN file.source IN ('AGENT_RESULT','INTEGRATION_RESULT')
              AND (
                (control_plane.runtime_file_coordinator(p_tenant,p_actor,p_project,p_agent,p_node)
                 AND EXISTS (SELECT 1 FROM control_plane.runtime_received_result_artifacts(p_tenant,p_node) pin
                     WHERE pin->>'ref'=file.ref AND pin->>'digest'=file.digest
                       AND pin->'revision'=to_jsonb(file.revision) AND pin->'version'=to_jsonb(coalesce(pinned.artifact_version,file.version))
                       AND pin->'sizeBytes'=to_jsonb(file.size_bytes) AND pin->>'fileName'=file.file_name
                       AND pin->>'mediaType'=file.media_type AND pin->>'source'=file.source))
                OR (NOT control_plane.runtime_file_coordinator(p_tenant,p_actor,p_project,p_agent,p_node)
                    AND 'platform.artifact.manage'=ANY(current_agent.capabilities)
                    AND NOT EXISTS (SELECT 1 FROM control_plane.run_nodes execution
                        WHERE execution.id=p_node AND execution.workflow_step_key LIKE 'workflow.coordinator.%'))
              )
              AND EXISTS (SELECT 1 FROM control_plane.catalog_access_targets run_target
                WHERE run_target.organization_id=p_tenant AND run_target.kind='RUN' AND run_target.id=file.run_id
                  AND control_plane.catalog_resource_visible(p_tenant,p_actor,'run.view','RUN',run_target.id,
                    run_target.project_id,run_target.owner_id,run_target.related_ids,statement_timestamp(),false))
            WHEN 'SKILL' THEN file.scan_state='CLEAN' AND EXISTS (
              SELECT 1 FROM control_plane.agent_context_bindings binding
              JOIN control_plane.skill_bundle_revisions revision ON revision.id=binding.skill_revision_id
              JOIN control_plane.skill_bundles bundle ON bundle.id=revision.bundle_id AND bundle.id=binding.skill_bundle_id
              WHERE binding.organization_id=p_tenant AND binding.project_id=p_project AND binding.agent_id=p_agent AND binding.enabled
                AND revision.ref=p_source_revision AND bundle.project_id=p_project AND bundle.state='ACTIVE'
                AND control_plane.skill_revision_visible(p_tenant,p_actor,revision.id,statement_timestamp()))
            ELSE false END
    );
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.runtime_file_source_visible_revision(uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.runtime_file_source_visible_revision(uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid) TO control_plane_runtime;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.runtime_file_source_visible(
 p_tenant uuid,p_actor uuid,p_project uuid,p_agent uuid,p_artifact uuid,p_purpose text,p_source_revision text,p_node uuid
) RETURNS boolean LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $$
 SELECT control_plane.runtime_file_source_visible_revision(p_tenant,p_actor,p_project,p_agent,p_artifact,p_purpose,p_source_revision,p_node,NULL::uuid);
$$;
-- +goose StatementEnd

-- FILE source provenance фиксируется в исходной Resolve transaction, до позднего private Prepare.
CREATE TABLE control_plane.kubernetes_file_input_origins (
 invocation_id uuid PRIMARY KEY REFERENCES control_plane.integration_invocations(id) DEFERRABLE INITIALLY IMMEDIATE,
 organization_id uuid NOT NULL REFERENCES control_plane.organizations(id) DEFERRABLE INITIALLY IMMEDIATE,
 runtime_revision_id uuid NOT NULL REFERENCES control_plane.runtime_revisions(id) DEFERRABLE INITIALLY IMMEDIATE,
 runtime_revision_ref text NOT NULL,
 runtime_revision_digest text NOT NULL CHECK(runtime_revision_digest ~ '^[a-f0-9]{64}$'),
 catalog_id uuid NOT NULL REFERENCES control_plane.runtime_file_catalogs(id) DEFERRABLE INITIALLY IMMEDIATE,
 catalog_ref text NOT NULL,
 catalog_digest text NOT NULL CHECK(catalog_digest ~ '^[a-f0-9]{64}$'),
 entry_id uuid NOT NULL REFERENCES control_plane.runtime_file_catalog_entries(id) DEFERRABLE INITIALLY IMMEDIATE,
 entry_ref text NOT NULL,
 entry_digest text NOT NULL CHECK(entry_digest ~ '^[a-f0-9]{64}$'),
 purpose text NOT NULL CHECK(purpose IN('PROJECT','WORKSPACE_INPUT','RUN_RESULT')),
 artifact_id uuid NOT NULL REFERENCES control_plane.artifact_heads(id) DEFERRABLE INITIALLY IMMEDIATE,
 artifact_ref text NOT NULL,
 artifact_revision_id uuid REFERENCES control_plane.artifact_revisions(id) DEFERRABLE INITIALLY IMMEDIATE,
 artifact_revision_ref text NOT NULL,
 artifact_revision bigint NOT NULL CHECK(artifact_revision>0),
 artifact_version bigint NOT NULL CHECK(artifact_version>0),
 artifact_digest text NOT NULL CHECK(artifact_digest ~ '^sha256:[a-f0-9]{64}$'),
 artifact_size_bytes bigint NOT NULL CHECK(artifact_size_bytes BETWEEN 0 AND 16777216),
 actor_id uuid NOT NULL REFERENCES control_plane.subjects(id) DEFERRABLE INITIALLY IMMEDIATE,
 project_id uuid NOT NULL REFERENCES control_plane.projects(id) DEFERRABLE INITIALLY IMMEDIATE,
 project_ref text NOT NULL,
 object_key text NOT NULL,
 object_version text NOT NULL CHECK(object_version<>'' AND lower(object_version)<>'null'),
 object_etag text NOT NULL CHECK(object_etag<>''),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(invocation_id,organization_id)
);
-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_kubernetes_file_origin() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM control_plane.integration_invocations invocation
  JOIN control_plane.run_nodes node ON node.id=invocation.node_id AND node.organization_id=invocation.organization_id
  JOIN control_plane.runs run ON run.id=invocation.run_id AND run.organization_id=invocation.organization_id
  JOIN control_plane.runs root ON root.id=run.root_run_id AND root.organization_id=run.organization_id
  JOIN control_plane.runtime_revisions revision ON revision.id=NEW.runtime_revision_id
   AND revision.organization_id=invocation.organization_id AND revision.ref=NEW.runtime_revision_ref
   AND revision.revision_digest=NEW.runtime_revision_digest AND revision.node_id=node.id
   AND revision.run_id=run.id AND revision.root_run_id=root.id AND revision.agent_id=node.agent_id
   AND revision.attempt=node.attempt AND revision.turn_id IS NOT DISTINCT FROM node.turn_id
   AND revision.created_at<=invocation.created_at
  JOIN control_plane.runtime_file_catalogs catalog ON catalog.id=NEW.catalog_id
   AND catalog.ref=NEW.catalog_ref AND catalog.digest=NEW.catalog_digest AND catalog.frozen
   AND catalog.runtime_revision_ref=revision.ref AND catalog.organization_id=invocation.organization_id
   AND catalog.actor_id=root.initiated_by AND catalog.actor_id=NEW.actor_id
   AND catalog.project_id=NEW.project_id AND catalog.node_id=node.id AND catalog.run_id=run.id
  JOIN control_plane.runtime_file_visible_entries entry ON entry.id=NEW.entry_id
   AND entry.ref=NEW.entry_ref AND entry.catalog_id=catalog.id AND entry.purpose=NEW.purpose
   AND entry.purpose=ANY(catalog.purposes) AND encode(entry.entry_digest,'hex')=NEW.entry_digest
   AND entry.artifact_id=NEW.artifact_id AND entry.artifact_ref=NEW.artifact_ref
   AND entry.artifact_revision=NEW.artifact_revision AND entry.artifact_version=NEW.artifact_version
   AND entry.artifact_digest=NEW.artifact_digest AND entry.size_bytes=NEW.artifact_size_bytes
   AND entry.project_ref=NEW.project_ref
  JOIN control_plane.artifact_history artifact ON artifact.id=entry.artifact_id AND artifact.revision=entry.artifact_revision
   AND artifact.revision_id=NEW.artifact_revision_id AND artifact.revision_ref=NEW.artifact_revision_ref
  JOIN control_plane.artifact_revision_content content ON content.revision_id=artifact.revision_id
   AND content.object_key=NEW.object_key AND content.object_version=NEW.object_version
   AND content.object_etag=NEW.object_etag AND content.digest=NEW.artifact_digest AND content.size_bytes=NEW.artifact_size_bytes
  WHERE invocation.id=NEW.invocation_id AND invocation.organization_id=NEW.organization_id
   AND invocation.operation='kubernetes.cp.upload' AND invocation.resource_kind='KUBERNETES_TARGET'
   AND invocation.generation=0 AND invocation.version=1
   AND invocation.bounded_input->'options'->>'artifactRef'=NEW.artifact_ref
   AND 'sha256:'||(invocation.bounded_input->'options'->>'artifactSHA256')=NEW.artifact_digest
 ) THEN RAISE EXCEPTION 'Kubernetes file source origin is invalid' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_kubernetes_file_origin() FROM PUBLIC;
CREATE TRIGGER kubernetes_file_origin_guard BEFORE INSERT ON control_plane.kubernetes_file_input_origins
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_kubernetes_file_origin();
-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_kubernetes_file_origin_release() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF control_plane.project_purge_authorized() THEN
  IF TG_OP='DELETE' THEN RETURN OLD; ELSE RETURN NEW; END IF;
 END IF;
 IF TG_OP='UPDATE' AND current_user='control_plane_owner' AND OLD.artifact_revision_id IS NOT NULL
  AND NEW.artifact_revision_id IS NULL
  AND(to_jsonb(NEW)-'artifact_revision_id')=(to_jsonb(OLD)-'artifact_revision_id')
  AND control_plane.kubernetes_file_origin_release_eligible(OLD.invocation_id,OLD.organization_id) THEN RETURN NEW; END IF;
 RAISE EXCEPTION 'Kubernetes FILE origin is immutable' USING ERRCODE='23514';
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_kubernetes_file_origin_release() FROM PUBLIC;
CREATE TRIGGER kubernetes_file_origin_immutable BEFORE UPDATE OR DELETE ON control_plane.kubernetes_file_input_origins
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_kubernetes_file_origin_release();
REVOKE ALL ON control_plane.kubernetes_file_input_origins FROM control_plane_runtime;
GRANT SELECT,INSERT ON control_plane.kubernetes_file_input_origins TO control_plane_runtime;

-- Один normalized private source, а не новая очередь: delivery остаётся в existing INPUT stage/claim/admit/ACK.
CREATE TABLE control_plane.kubernetes_file_input_sources (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 ref text NOT NULL UNIQUE CHECK(ref ~ '^kfs_[A-Za-z0-9_-]{8,89}$'),
 organization_id uuid NOT NULL REFERENCES control_plane.organizations(id) DEFERRABLE INITIALLY IMMEDIATE,
 invocation_id uuid NOT NULL,
 child_id uuid NOT NULL,
 plan_id uuid NOT NULL,
 actor_id uuid NOT NULL REFERENCES control_plane.subjects(id) DEFERRABLE INITIALLY IMMEDIATE,
 project_id uuid NOT NULL REFERENCES control_plane.projects(id) DEFERRABLE INITIALLY IMMEDIATE,
 snapshot_digest text NOT NULL CHECK(snapshot_digest ~ '^[a-f0-9]{64}$'),
 binding_digest text NOT NULL CHECK(binding_digest ~ '^[a-f0-9]{64}$'),
 child_input_digest text NOT NULL CHECK(child_input_digest ~ '^[a-f0-9]{64}$'),
 invocation_generation bigint NOT NULL CHECK(invocation_generation>0),
 original_fence_digest text NOT NULL CHECK(original_fence_digest ~ '^[a-f0-9]{64}$'),
 stream_id uuid UNIQUE,
 state text NOT NULL CHECK(state IN('PREPARING','READY','UNKNOWN','REJECTED','CANCELLED','TRANSFERRED','EXPIRED')),
 reason_code text NOT NULL DEFAULT 'NONE' CHECK(reason_code IN('NONE','SOURCE_UNAVAILABLE','SOURCE_CHANGED','PERMISSION_LOST','CONTENT_REJECTED','INPUT_UNKNOWN','CANCELLED','EXPIRED')),
 version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 maximum_bytes bigint NOT NULL CHECK(maximum_bytes BETWEEN 1 AND 16777216),
 transfer_format text NOT NULL CHECK(transfer_format IN('FILE','TAR')),
 hard_deadline timestamptz NOT NULL,
 writer_deadline timestamptz NOT NULL,
 writer_completed_at timestamptz,
 authorized_until timestamptz,
 source_access_until timestamptz,
 prepared_content_id uuid UNIQUE REFERENCES control_plane.prepared_content(id) DEFERRABLE INITIALLY IMMEDIATE,
 payload_digest text CHECK(payload_digest ~ '^[a-f0-9]{64}$'),
 byte_count bigint CHECK(byte_count BETWEEN 1 AND 16777216),
 normalization_revision integer CHECK(normalization_revision=1),
 manifest text CHECK(octet_length(manifest)<=65536),
 manifest_digest text CHECK(manifest_digest ~ '^[a-f0-9]{64}$'),
 object_key text,
 object_version text,
 object_etag text,
 cleanup_generation bigint NOT NULL DEFAULT 0 CHECK(cleanup_generation>=0),
 cleanup_owner text,
 cleanup_deadline timestamptz,
 cleanup_attempts integer NOT NULL DEFAULT 0 CHECK(cleanup_attempts BETWEEN 0 AND 3),
 cleanup_total_attempts bigint NOT NULL DEFAULT 0 CHECK(cleanup_total_attempts>=cleanup_attempts),
 cleanup_repair_generation bigint NOT NULL DEFAULT 0 CHECK(cleanup_repair_generation>=0),
 cleanup_version bigint NOT NULL DEFAULT 1 CHECK(cleanup_version>0),
 cleanup_object_version text,
 cleanup_object_etag text,
 payload_purged_at timestamptz,
 deletion_receipt_digest text CHECK(deletion_receipt_digest ~ '^[a-f0-9]{64}$'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(id,organization_id),UNIQUE(child_id,organization_id),UNIQUE(id,child_id,organization_id),
 FOREIGN KEY(invocation_id,organization_id) REFERENCES control_plane.kubernetes_file_input_origins(invocation_id,organization_id) DEFERRABLE INITIALLY IMMEDIATE,
 FOREIGN KEY(plan_id,organization_id) REFERENCES control_plane.kubernetes_execution_plans(id,organization_id) DEFERRABLE INITIALLY IMMEDIATE,
 FOREIGN KEY(child_id,organization_id) REFERENCES control_plane.kubernetes_execution_children(id,organization_id) DEFERRABLE INITIALLY IMMEDIATE,
 FOREIGN KEY(stream_id,organization_id) REFERENCES control_plane.kubernetes_execution_streams(id,organization_id) DEFERRABLE INITIALLY IMMEDIATE,
 CHECK(writer_deadline<=hard_deadline),CHECK(authorized_until IS NULL OR authorized_until<=hard_deadline),
 CHECK((cleanup_owner IS NULL)=(cleanup_deadline IS NULL)),
 CHECK((cleanup_object_version IS NULL)=(cleanup_object_etag IS NULL)),
 CHECK((payload_purged_at IS NULL)=(deletion_receipt_digest IS NULL)),
 CHECK((payload_digest IS NULL)=(byte_count IS NULL) AND(payload_digest IS NULL)=(manifest IS NULL)
  AND(payload_digest IS NULL)=(manifest_digest IS NULL) AND(payload_digest IS NULL)=(normalization_revision IS NULL)),
 CHECK((object_key IS NULL)=(object_version IS NULL) AND(object_key IS NULL)=(object_etag IS NULL)),
 CHECK(state NOT IN('READY','TRANSFERRED') OR(prepared_content_id IS NOT NULL AND payload_digest IS NOT NULL
  AND byte_count IS NOT NULL AND byte_count<=maximum_bytes AND normalization_revision IS NOT NULL AND normalization_revision=1
  AND manifest IS NOT NULL AND manifest_digest IS NOT NULL AND object_key IS NOT NULL
  AND object_version IS NOT NULL AND object_version<>'' AND lower(object_version)<>'null'
  AND object_etag IS NOT NULL AND object_etag<>'' AND writer_completed_at IS NOT NULL))
);
-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_kubernetes_file_source() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.state<>'PREPARING' OR NEW.version<>1 OR NEW.stream_id IS NOT NULL OR NEW.prepared_content_id IS NOT NULL
   OR NEW.payload_digest IS NOT NULL OR NEW.byte_count IS NOT NULL OR NEW.manifest IS NOT NULL
   OR NEW.object_key IS NOT NULL OR NEW.object_version IS NOT NULL OR NEW.object_etag IS NOT NULL
   OR NEW.writer_completed_at IS NOT NULL OR NEW.authorized_until IS NOT NULL OR NEW.source_access_until IS NOT NULL
   OR NEW.cleanup_generation<>0 OR NEW.cleanup_attempts<>0 OR NEW.cleanup_total_attempts<>0 OR NEW.cleanup_repair_generation<>0 OR NEW.cleanup_version<>1
   OR NEW.cleanup_owner IS NOT NULL OR NEW.cleanup_deadline IS NOT NULL OR NEW.cleanup_object_version IS NOT NULL
   OR NEW.cleanup_object_etag IS NOT NULL OR NEW.payload_purged_at IS NOT NULL OR NEW.deletion_receipt_digest IS NOT NULL
   OR NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_execution_children child
    JOIN control_plane.kubernetes_execution_plans plan ON plan.id=child.plan_id AND plan.organization_id=child.organization_id
    JOIN control_plane.integration_invocations invocation ON invocation.id=plan.invocation_id AND invocation.organization_id=plan.organization_id
    JOIN control_plane.kubernetes_file_input_origins origin ON origin.invocation_id=invocation.id AND origin.organization_id=invocation.organization_id
    WHERE child.id=NEW.child_id AND child.organization_id=NEW.organization_id AND plan.id=NEW.plan_id
     AND invocation.id=NEW.invocation_id AND invocation.state='RUNNING' AND child.operation='kubernetes.cp.upload'
     AND origin.actor_id=NEW.actor_id AND origin.project_id=NEW.project_id AND origin.artifact_size_bytes<=NEW.maximum_bytes
     AND invocation.generation=NEW.invocation_generation AND invocation.effect_fence_digest=NEW.original_fence_digest
     AND plan.snapshot_digest=NEW.snapshot_digest AND plan.binding_digest=NEW.binding_digest AND child.input_digest=NEW.child_input_digest
     AND NEW.maximum_bytes=(child.input_json::jsonb->'options'->>'maximumBytes')::bigint
     AND NEW.transfer_format=child.input_json::jsonb->'options'->>'transferFormat'
     AND origin.artifact_ref=child.input_json::jsonb->'options'->>'artifactRef'
     AND origin.artifact_digest='sha256:'||(child.input_json::jsonb->'options'->>'artifactSHA256')
     AND NEW.hard_deadline=plan.hard_deadline AND NEW.writer_deadline<=invocation.lease_expires_at
     AND NEW.writer_deadline>clock_timestamp() AND NEW.writer_deadline<=clock_timestamp()+interval '45 seconds'
     AND control_plane.kubernetes_execution_child_grant_current(child.id,invocation.id)) THEN
   RAISE EXCEPTION 'Kubernetes FILE source intent is invalid' USING ERRCODE='23514';
  END IF;
 ELSE
  IF OLD.payload_purged_at IS NOT NULL AND NEW IS DISTINCT FROM OLD THEN
   RAISE EXCEPTION 'Kubernetes FILE purged source is immutable' USING ERRCODE='23514'; END IF;
  IF NEW.version<>OLD.version OR (OLD.version=9223372036854775807 AND (to_jsonb(NEW)-ARRAY['version','cleanup_generation','cleanup_owner','cleanup_deadline','cleanup_attempts','cleanup_total_attempts','cleanup_repair_generation','cleanup_version','cleanup_object_version','cleanup_object_etag','payload_purged_at','deletion_receipt_digest']) IS DISTINCT FROM (to_jsonb(OLD)-ARRAY['version','cleanup_generation','cleanup_owner','cleanup_deadline','cleanup_attempts','cleanup_total_attempts','cleanup_repair_generation','cleanup_version','cleanup_object_version','cleanup_object_etag','payload_purged_at','deletion_receipt_digest'])) OR
   (to_jsonb(NEW)-ARRAY['version','stream_id','state','reason_code','writer_completed_at','authorized_until','source_access_until','prepared_content_id',
    'payload_digest','byte_count','normalization_revision','manifest','manifest_digest','object_key','object_version','object_etag',
    'cleanup_generation','cleanup_owner','cleanup_deadline','cleanup_attempts','cleanup_total_attempts','cleanup_repair_generation','cleanup_version',
    'cleanup_object_version','cleanup_object_etag','payload_purged_at','deletion_receipt_digest']) IS DISTINCT FROM
   (to_jsonb(OLD)-ARRAY['version','stream_id','state','reason_code','writer_completed_at','authorized_until','source_access_until','prepared_content_id',
    'payload_digest','byte_count','normalization_revision','manifest','manifest_digest','object_key','object_version','object_etag',
    'cleanup_generation','cleanup_owner','cleanup_deadline','cleanup_attempts','cleanup_total_attempts','cleanup_repair_generation','cleanup_version',
    'cleanup_object_version','cleanup_object_etag','payload_purged_at','deletion_receipt_digest']) OR
   OLD.stream_id IS NOT NULL AND NEW.stream_id IS DISTINCT FROM OLD.stream_id OR
   OLD.stream_id IS NOT NULL AND NEW.source_access_until IS DISTINCT FROM OLD.source_access_until OR
   OLD.writer_completed_at IS NOT NULL AND NEW.writer_completed_at IS DISTINCT FROM OLD.writer_completed_at OR
   OLD.prepared_content_id IS NOT NULL AND NEW.prepared_content_id IS DISTINCT FROM OLD.prepared_content_id OR
   OLD.payload_digest IS NOT NULL AND ROW(NEW.payload_digest,NEW.byte_count,NEW.normalization_revision,NEW.manifest,NEW.manifest_digest)
    IS DISTINCT FROM ROW(OLD.payload_digest,OLD.byte_count,OLD.normalization_revision,OLD.manifest,OLD.manifest_digest) OR
   OLD.object_version IS NOT NULL AND ROW(NEW.object_key,NEW.object_version,NEW.object_etag)
    IS DISTINCT FROM ROW(OLD.object_key,OLD.object_version,OLD.object_etag) OR
   NOT(NEW.state=OLD.state OR OLD.state='PREPARING' AND NEW.state IN('READY','UNKNOWN','REJECTED','CANCELLED','EXPIRED')
    OR OLD.state='READY' AND NEW.state IN('TRANSFERRED','UNKNOWN','CANCELLED','EXPIRED')
    OR OLD.state='UNKNOWN' AND OLD.reason_code<>'INPUT_UNKNOWN' AND NEW.state IN('READY','REJECTED','CANCELLED','EXPIRED')) THEN
   RAISE EXCEPTION 'Kubernetes FILE source fact is immutable' USING ERRCODE='23514';
  END IF;
  IF ROW(NEW.cleanup_generation,NEW.cleanup_owner,NEW.cleanup_deadline,NEW.cleanup_attempts,NEW.cleanup_total_attempts,NEW.cleanup_repair_generation,
    NEW.cleanup_object_version,NEW.cleanup_object_etag,NEW.payload_purged_at,NEW.deletion_receipt_digest)
    IS DISTINCT FROM ROW(OLD.cleanup_generation,OLD.cleanup_owner,OLD.cleanup_deadline,OLD.cleanup_attempts,OLD.cleanup_total_attempts,OLD.cleanup_repair_generation,
    OLD.cleanup_object_version,OLD.cleanup_object_etag,OLD.payload_purged_at,OLD.deletion_receipt_digest)
   AND(current_user<>'control_plane_owner' OR NEW.cleanup_generation<OLD.cleanup_generation OR NEW.cleanup_total_attempts<OLD.cleanup_total_attempts
    OR NEW.cleanup_repair_generation<OLD.cleanup_repair_generation OR OLD.payload_purged_at IS NOT NULL
    OR OLD.cleanup_object_version IS NOT NULL AND ROW(NEW.cleanup_object_version,NEW.cleanup_object_etag)
      IS DISTINCT FROM ROW(OLD.cleanup_object_version,OLD.cleanup_object_etag)) THEN
   RAISE EXCEPTION 'Kubernetes FILE cleanup receipt is invalid' USING ERRCODE='23514';
  END IF;
  IF (to_jsonb(NEW)-ARRAY['version','cleanup_generation','cleanup_owner','cleanup_deadline','cleanup_attempts','cleanup_total_attempts','cleanup_repair_generation','cleanup_version','cleanup_object_version','cleanup_object_etag','payload_purged_at','deletion_receipt_digest']) IS DISTINCT FROM (to_jsonb(OLD)-ARRAY['version','cleanup_generation','cleanup_owner','cleanup_deadline','cleanup_attempts','cleanup_total_attempts','cleanup_repair_generation','cleanup_version','cleanup_object_version','cleanup_object_etag','payload_purged_at','deletion_receipt_digest']) THEN
   NEW.version:=OLD.version+1;
  END IF;
 END IF;
 IF NEW.payload_digest IS NOT NULL THEN
  IF NEW.byte_count IS NULL OR NEW.byte_count>NEW.maximum_bytes OR NEW.normalization_revision IS DISTINCT FROM 1 OR NEW.manifest IS NULL
   OR NEW.manifest_digest IS NULL OR encode(public.digest(convert_to(NEW.manifest,'UTF8'),'sha256'),'hex')<>NEW.manifest_digest
   OR jsonb_typeof(NEW.manifest::jsonb) IS DISTINCT FROM 'object'
   OR(SELECT count(*) FROM jsonb_object_keys(NEW.manifest::jsonb))<>5
   OR(NEW.manifest::jsonb->>'sha256') IS DISTINCT FROM NEW.payload_digest
   OR(NEW.manifest::jsonb->>'sizeBytes')::bigint IS DISTINCT FROM NEW.byte_count OR(NEW.manifest::jsonb->>'frameBytes')::integer IS DISTINCT FROM 32768
   OR(NEW.manifest::jsonb->>'normalizationRevision')::integer IS DISTINCT FROM 1
   OR jsonb_typeof(NEW.manifest::jsonb->'chunkSHA256') IS DISTINCT FROM 'array'
   OR jsonb_array_length(NEW.manifest::jsonb->'chunkSHA256')<>(NEW.byte_count+32767)/32768
   OR EXISTS(SELECT 1 FROM jsonb_array_elements_text(NEW.manifest::jsonb->'chunkSHA256') digest WHERE digest !~ '^[a-f0-9]{64}$') THEN
   RAISE EXCEPTION 'Kubernetes FILE normalized manifest is invalid' USING ERRCODE='23514';
  END IF;
 END IF;
 IF NEW.payload_purged_at IS NULL AND NEW.prepared_content_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM control_plane.prepared_content content
  WHERE content.id=NEW.prepared_content_id AND content.organization_id=NEW.organization_id AND content.actor_id=NEW.actor_id
   AND content.source_profile='KUBERNETES_FILE_SOURCE' AND content.source_profile_ref=NEW.ref
   AND content.source_context_digest=NEW.snapshot_digest AND content.source_profile_version=NEW.invocation_generation
   AND content.source_fence_digest=NEW.original_fence_digest AND content.source_revision_ref=(SELECT origin.artifact_revision_ref
    FROM control_plane.kubernetes_file_input_origins origin WHERE origin.invocation_id=NEW.invocation_id AND origin.organization_id=NEW.organization_id)
   AND content.digest='sha256:'||NEW.payload_digest AND content.size_bytes=NEW.byte_count) THEN
  RAISE EXCEPTION 'Kubernetes FILE prepared owner mismatch' USING ERRCODE='23514';
 END IF;
 IF NEW.payload_purged_at IS NULL AND NEW.state IN('READY','TRANSFERRED') AND NOT EXISTS(SELECT 1 FROM control_plane.prepared_content content
  WHERE content.id=NEW.prepared_content_id AND content.organization_id=NEW.organization_id AND content.state IN('STAGED','ADOPTED')
   AND content.object_key=NEW.object_key AND content.object_version=NEW.object_version AND content.object_etag=NEW.object_etag
   AND content.scan_state IN('CLEAN','NOT_SCANNED') AND content.deletion_receipt_digest IS NULL) THEN
  RAISE EXCEPTION 'Kubernetes FILE source has no accepted prepared receipt' USING ERRCODE='23514';
 END IF;
 IF NEW.stream_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_execution_streams stream
  JOIN control_plane.kubernetes_execution_effects effect ON effect.id=stream.effect_id AND effect.organization_id=stream.organization_id
  JOIN control_plane.kubernetes_execution_children child ON child.id=effect.child_id AND child.organization_id=effect.organization_id
  JOIN control_plane.kubernetes_execution_plans plan ON plan.id=child.plan_id AND plan.organization_id=child.organization_id
  WHERE stream.id=NEW.stream_id AND stream.organization_id=NEW.organization_id AND child.id=NEW.child_id
   AND plan.id=NEW.plan_id AND plan.invocation_id=NEW.invocation_id
   AND stream.generation=NEW.invocation_generation AND plan.binding_digest=NEW.binding_digest) THEN
  RAISE EXCEPTION 'Kubernetes FILE stream owner mismatch' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_kubernetes_file_source() FROM PUBLIC;
CREATE TRIGGER kubernetes_file_source_guard BEFORE INSERT OR UPDATE ON control_plane.kubernetes_file_input_sources
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_kubernetes_file_source();
-- +goose StatementBegin
CREATE FUNCTION control_plane.verify_kubernetes_file_adoption() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM control_plane.kubernetes_file_input_sources source
  LEFT JOIN control_plane.prepared_content content ON content.id=source.prepared_content_id AND content.organization_id=source.organization_id
  WHERE source.id=NEW.id AND source.organization_id=NEW.organization_id AND source.state IN('READY','TRANSFERRED')
   AND(source.payload_purged_at IS NULL) AND(content.state IS DISTINCT FROM 'ADOPTED' OR content.adopted_file_source_id IS DISTINCT FROM source.id)) THEN
  RAISE EXCEPTION 'Kubernetes FILE adoption is incomplete' USING ERRCODE='23514';
 END IF;
 RETURN NULL;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.verify_kubernetes_file_adoption() FROM PUBLIC;
CREATE CONSTRAINT TRIGGER kubernetes_file_source_adoption AFTER INSERT OR UPDATE ON control_plane.kubernetes_file_input_sources
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION control_plane.verify_kubernetes_file_adoption();
CREATE INDEX kubernetes_file_source_quota ON control_plane.kubernetes_file_input_sources(organization_id) WHERE payload_purged_at IS NULL;
CREATE INDEX kubernetes_file_source_cleanup ON control_plane.kubernetes_file_input_sources(created_at,id) WHERE payload_purged_at IS NULL;
ALTER TABLE control_plane.prepared_content ADD COLUMN adopted_file_source_id uuid
 REFERENCES control_plane.kubernetes_file_input_sources(id) DEFERRABLE INITIALLY IMMEDIATE;
-- Existing ADOPTED artifact contracts сохраняются; FILE adoption имеет отдельную typed owner edge.
-- +goose StatementBegin
DO $file_adopted_check$
DECLARE candidate text; matches integer;
BEGIN
 SELECT count(*),min(conname) INTO matches,candidate FROM pg_constraint
 WHERE conrelid='control_plane.prepared_content'::regclass AND contype='c'
  AND pg_get_constraintdef(oid) LIKE '%state <> ''ADOPTED''%'
  AND pg_get_constraintdef(oid) LIKE '%adopted_revision_ref IS NOT NULL%';
 IF matches<>1 THEN RAISE EXCEPTION 'Prepared FILE adoption predecessor is unexpected'; END IF;
 EXECUTE format('ALTER TABLE control_plane.prepared_content DROP CONSTRAINT %I',candidate);
 ALTER TABLE control_plane.prepared_content ADD CONSTRAINT prepared_content_adopted_owner_check
 CHECK(state<>'ADOPTED' OR adopted_revision_ref IS NOT NULL OR(source_profile='KUBERNETES_FILE_SOURCE' AND adopted_file_source_id IS NOT NULL));
END $file_adopted_check$;
-- +goose StatementEnd
ALTER TABLE control_plane.prepared_content DROP CONSTRAINT prepared_content_source_profile_check;
ALTER TABLE control_plane.prepared_content ADD CONSTRAINT prepared_content_source_profile_check CHECK(source_profile IN('SYSTEM','PROJECT','KUBERNETES_OUTPUT','KUBERNETES_FILE_SOURCE'));
ALTER TABLE control_plane.prepared_content DROP CONSTRAINT prepared_content_size_bytes_check;
ALTER TABLE control_plane.prepared_content ADD CONSTRAINT prepared_content_size_bytes_check
 CHECK(size_bytes>=0 AND size_bytes<=CASE source_profile WHEN 'KUBERNETES_OUTPUT' THEN 536870912 WHEN 'KUBERNETES_FILE_SOURCE' THEN 16777216 ELSE 1048576 END);
REVOKE ALL ON control_plane.kubernetes_file_input_sources FROM control_plane_runtime;
GRANT SELECT,INSERT,UPDATE ON control_plane.kubernetes_file_input_sources TO control_plane_runtime;

CREATE UNIQUE INDEX prepared_content_kubernetes_file_source_owner
 ON control_plane.prepared_content(organization_id,source_profile_ref) WHERE source_profile='KUBERNETES_FILE_SOURCE';

-- Typed FILE adoption не выдаёт public ArtifactRevision и не меняет ordinary prepared lifecycle.
-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_kubernetes_file_prepared() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF NEW.source_profile<>'KUBERNETES_FILE_SOURCE' THEN
  IF NEW.adopted_file_source_id IS NOT NULL THEN RAISE EXCEPTION 'Unexpected FILE adoption owner' USING ERRCODE='23514'; END IF;
  RETURN NEW;
 END IF;
 IF TG_OP='UPDATE' AND OLD.deletion_receipt_digest IS NULL AND NEW.deletion_receipt_digest IS NOT NULL THEN
  IF current_user<>'control_plane_owner' OR OLD.state NOT IN('ADOPTED','CLEANING')
   OR NEW.state IS DISTINCT FROM (CASE WHEN OLD.state='ADOPTED' THEN 'ADOPTED' ELSE 'CLEANED' END)
   OR NEW.adopted_file_source_id IS DISTINCT FROM OLD.adopted_file_source_id
   OR NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_file_input_sources source
    WHERE source.ref=OLD.source_profile_ref AND source.organization_id=OLD.organization_id
     AND(source.prepared_content_id IS NULL OR source.prepared_content_id=OLD.id)
     AND(OLD.adopted_file_source_id IS NULL OR OLD.adopted_file_source_id=source.id)
     AND source.payload_purged_at IS NOT NULL AND source.deletion_receipt_digest=NEW.deletion_receipt_digest
     AND source.cleanup_generation>0 AND source.cleanup_owner IS NULL AND source.cleanup_deadline IS NULL) THEN
   RAISE EXCEPTION 'Kubernetes FILE terminal prepared receipt mismatch' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 -- Generic prepared collection не выдаёт FILE cleanup credit вне source owner.
 IF TG_OP='UPDATE' AND ROW(NEW.cleanup_owner,NEW.cleanup_deadline,NEW.cleanup_attempts)
  IS DISTINCT FROM ROW(OLD.cleanup_owner,OLD.cleanup_deadline,OLD.cleanup_attempts)
  AND(current_user<>'control_plane_owner' OR NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_file_input_sources source
   WHERE source.ref=OLD.source_profile_ref AND source.organization_id=OLD.organization_id AND source.payload_purged_at IS NULL
    AND(source.prepared_content_id IS NULL OR source.prepared_content_id=OLD.id)
    AND source.cleanup_generation>0 AND source.cleanup_owner IS NOT NULL AND source.cleanup_deadline>clock_timestamp()
    AND NEW.cleanup_attempts=source.cleanup_attempts
    AND(NEW.cleanup_owner=source.cleanup_owner AND NEW.cleanup_deadline=source.cleanup_deadline
     OR NEW.cleanup_owner IS NULL AND NEW.cleanup_deadline IS NULL
      AND OLD.cleanup_owner=source.cleanup_owner AND OLD.cleanup_deadline=source.cleanup_deadline))) THEN
  RAISE EXCEPTION 'Kubernetes FILE prepared cleanup lacks source owner' USING ERRCODE='23514'; END IF;
 IF NEW.intent_operation<>'kubernetes.file.prepare' OR NEW.operation_key<>'kubernetes.cp.upload'
  OR NEW.idempotency_key<>NEW.source_profile_ref OR NEW.available_until<>'infinity'::timestamptz
  OR NEW.target_artifact_id IS NOT NULL OR NEW.target_artifact_ref<>'' OR NEW.expected_artifact_version<>0
  OR NEW.adopted_revision_ref IS NOT NULL OR NEW.adopted_revision_id IS NOT NULL
  OR NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_file_input_sources source
   JOIN control_plane.kubernetes_file_input_origins origin ON origin.invocation_id=source.invocation_id AND origin.organization_id=source.organization_id
   JOIN control_plane.integration_invocations invocation ON invocation.id=source.invocation_id AND invocation.organization_id=source.organization_id
   JOIN control_plane.runs run ON run.id=invocation.run_id AND run.organization_id=invocation.organization_id
   WHERE source.ref=NEW.source_profile_ref AND source.organization_id=NEW.organization_id
    AND source.actor_id=NEW.actor_id AND source.project_id=NEW.project_id AND origin.project_ref=NEW.origin_project_ref
    AND source.invocation_generation=NEW.source_profile_version AND source.snapshot_digest=NEW.source_context_digest
    AND source.original_fence_digest=NEW.source_fence_digest AND source.invocation_generation=NEW.source_lease_generation
    AND run.ref=NEW.source_run_ref
    AND(TG_OP<>'INSERT' OR invocation.lease_ref=NEW.source_lease_ref)
    AND origin.artifact_revision_ref=NEW.source_revision_ref
    AND source.payload_digest IS NOT NULL AND NEW.digest='sha256:'||source.payload_digest AND NEW.size_bytes=source.byte_count
    AND(source.prepared_content_id IS NULL OR source.prepared_content_id=NEW.id)
    AND(NEW.adopted_file_source_id IS NULL OR NEW.adopted_file_source_id=source.id)
    AND(NEW.state<>'ADOPTED' OR NEW.adopted_file_source_id=source.id AND source.prepared_content_id=NEW.id
      AND source.object_key=NEW.object_key AND source.object_version=NEW.object_version AND source.object_etag=NEW.object_etag)
    AND(TG_OP<>'INSERT' OR source.state='PREPARING' AND source.payload_purged_at IS NULL AND source.writer_completed_at IS NULL
      AND source.writer_deadline>clock_timestamp() AND invocation.state='RUNNING'
      AND invocation.generation=source.invocation_generation AND invocation.effect_fence_digest=source.original_fence_digest
      AND control_plane.kubernetes_execution_child_grant_current(source.child_id,invocation.id))) THEN
  RAISE EXCEPTION 'Kubernetes FILE prepared source mismatch' USING ERRCODE='23514';
 END IF;
 IF TG_OP='UPDATE' AND OLD.adopted_file_source_id IS NOT NULL AND NEW.adopted_file_source_id IS DISTINCT FROM OLD.adopted_file_source_id THEN
  RAISE EXCEPTION 'Kubernetes FILE adoption is immutable' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_kubernetes_file_prepared() FROM PUBLIC;
CREATE TRIGGER kubernetes_file_prepared_guard BEFORE INSERT OR UPDATE ON control_plane.prepared_content
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_kubernetes_file_prepared();
-- +goose StatementBegin
DO $file_prepared_protect$
DECLARE definition text; marker text; replacement text;
BEGIN
 SELECT pg_get_functiondef('control_plane.protect_prepared_content()'::regprocedure) INTO definition;
 marker:='''origin_plan_ref'',''origin_conversation_ref'',''origin_plan_revision_ref'',''origin_plan_revision'',''adopted_revision_id'',''adopted_revision_ref'',';
 IF (char_length(definition)-char_length(replace(definition,marker,'')))/char_length(marker)<>2 THEN
  RAISE EXCEPTION 'Prepared FILE protect predecessor is unexpected';
 END IF;
 definition:=replace(definition,marker,marker||'''adopted_file_source_id'',');
 marker:='(OLD.state=''ADOPTED'' AND EXISTS (';
 IF (char_length(definition)-char_length(replace(definition,marker,'')))/char_length(marker)<>1 THEN
  RAISE EXCEPTION 'Prepared FILE terminal predecessor is unexpected';
 END IF;
 replacement:='(OLD.state=''ADOPTED'' AND OLD.source_profile=''KUBERNETES_FILE_SOURCE'' AND EXISTS (SELECT 1 FROM control_plane.kubernetes_file_input_sources source WHERE source.id=OLD.adopted_file_source_id AND source.organization_id=OLD.organization_id AND source.prepared_content_id=OLD.id AND source.payload_purged_at IS NOT NULL AND source.deletion_receipt_digest=NEW.deletion_receipt_digest AND source.cleanup_generation>0 AND source.cleanup_owner IS NULL AND source.cleanup_deadline IS NULL AND source.state IN(''TRANSFERRED'',''REJECTED'',''CANCELLED'',''EXPIRED'',''UNKNOWN''))) OR ';
 definition:=replace(definition,marker,replacement||marker);
 EXECUTE definition;
END $file_prepared_protect$;
-- +goose StatementEnd

-- FILE frame привязывается к immutable normalized chunk в existing INPUT ledger.
ALTER TABLE control_plane.kubernetes_input_stages ADD CONSTRAINT kubernetes_input_stage_source_tuple UNIQUE(id,stream_id,organization_id);
CREATE TABLE control_plane.kubernetes_file_input_frames (
 source_id uuid NOT NULL,
 stream_id uuid NOT NULL,
 organization_id uuid NOT NULL,
 stage_id uuid NOT NULL UNIQUE,
 ordinal integer NOT NULL CHECK(ordinal BETWEEN 0 AND 511),
 byte_offset bigint NOT NULL CHECK(byte_offset>=0),
 byte_count integer NOT NULL CHECK(byte_count BETWEEN 1 AND 32768),
 payload_digest text NOT NULL CHECK(payload_digest ~ '^[a-f0-9]{64}$'),
 end_of_input boolean NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(source_id,ordinal),
 FOREIGN KEY(source_id,organization_id) REFERENCES control_plane.kubernetes_file_input_sources(id,organization_id) DEFERRABLE INITIALLY IMMEDIATE,
 FOREIGN KEY(stage_id,stream_id,organization_id) REFERENCES control_plane.kubernetes_input_stages(id,stream_id,organization_id) DEFERRABLE INITIALLY IMMEDIATE
);
-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_kubernetes_file_frame() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_file_input_sources source
  JOIN control_plane.kubernetes_input_stages stage ON stage.id=NEW.stage_id AND stage.organization_id=source.organization_id
  JOIN control_plane.kubernetes_execution_streams stream ON stream.id=stage.stream_id AND stream.organization_id=stage.organization_id
  WHERE source.id=NEW.source_id AND source.organization_id=NEW.organization_id AND source.stream_id=NEW.stream_id
   AND stage.stream_id=NEW.stream_id AND stage.channel='FILE_CONTENT' AND stage.state='ATTEMPTING'
   AND stage.object_key IS NULL AND stage.object_version IS NULL AND stage.object_etag IS NULL
   AND stage.actor_id=source.actor_id AND source.state='READY' AND source.payload_purged_at IS NULL
   AND source.authorized_until>clock_timestamp() AND source.hard_deadline>clock_timestamp()
   AND stream.state='ACTIVE' AND stage.byte_count=NEW.byte_count AND stage.payload_digest=NEW.payload_digest
   AND stage.end_of_input=NEW.end_of_input AND NEW.ordinal<jsonb_array_length(source.manifest::jsonb->'chunkSHA256')
   AND NEW.byte_offset=NEW.ordinal::bigint*32768
   AND NEW.byte_count=least(32768,source.byte_count-NEW.byte_offset)
   AND NEW.payload_digest=source.manifest::jsonb->'chunkSHA256'->>NEW.ordinal
   AND NEW.end_of_input=(NEW.byte_offset+NEW.byte_count=source.byte_count)
   AND NEW.ordinal=(SELECT count(*) FROM control_plane.kubernetes_file_input_frames prior WHERE prior.source_id=source.id)
   AND NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_file_input_frames prior
    JOIN control_plane.kubernetes_input_stages pending ON pending.id=prior.stage_id AND pending.organization_id=prior.organization_id
    WHERE prior.source_id=source.id AND pending.state<>'ACKNOWLEDGED')) THEN
  RAISE EXCEPTION 'Kubernetes FILE frame source mismatch' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_kubernetes_file_frame() FROM PUBLIC;
CREATE TRIGGER kubernetes_file_frame_guard BEFORE INSERT ON control_plane.kubernetes_file_input_frames
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_kubernetes_file_frame();
CREATE TRIGGER kubernetes_file_frame_immutable BEFORE UPDATE OR DELETE ON control_plane.kubernetes_file_input_frames
 FOR EACH ROW WHEN(NOT control_plane.project_purge_authorized()) EXECUTE FUNCTION control_plane.reject_integration_immutable_update();
-- +goose StatementBegin
CREATE FUNCTION control_plane.verify_kubernetes_file_frame() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF NEW.channel='FILE_CONTENT' AND NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_file_input_frames frame
  WHERE frame.stage_id=NEW.id AND frame.stream_id=NEW.stream_id AND frame.organization_id=NEW.organization_id) THEN
  RAISE EXCEPTION 'Kubernetes FILE input has no immutable source' USING ERRCODE='23514';
 END IF;
 RETURN NULL;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.verify_kubernetes_file_frame() FROM PUBLIC;
CREATE CONSTRAINT TRIGGER kubernetes_file_frame_binding AFTER INSERT ON control_plane.kubernetes_input_stages
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION control_plane.verify_kubernetes_file_frame();
-- +goose StatementBegin
CREATE FUNCTION control_plane.record_kubernetes_file_ack() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF NEW.state='ACKNOWLEDGED' AND OLD.state IS DISTINCT FROM NEW.state AND NEW.channel='FILE_CONTENT' THEN
  UPDATE control_plane.kubernetes_file_input_sources source SET state='TRANSFERRED',reason_code='NONE'
  FROM control_plane.kubernetes_file_input_frames frame
  WHERE frame.stage_id=NEW.id AND frame.organization_id=NEW.organization_id AND frame.source_id=source.id
   AND source.organization_id=frame.organization_id AND source.state='READY' AND frame.end_of_input
   AND NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_file_input_frames prior
    JOIN control_plane.kubernetes_input_stages pending ON pending.id=prior.stage_id AND pending.organization_id=prior.organization_id
    WHERE prior.source_id=source.id AND pending.state<>'ACKNOWLEDGED');
 END IF;
 RETURN NULL;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.record_kubernetes_file_ack() FROM PUBLIC;
CREATE TRIGGER kubernetes_file_ack AFTER UPDATE OF state ON control_plane.kubernetes_input_stages
 FOR EACH ROW EXECUTE FUNCTION control_plane.record_kubernetes_file_ack();
-- +goose StatementBegin
CREATE FUNCTION control_plane.close_kubernetes_file_source() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF TG_RELID='control_plane.kubernetes_execution_streams'::regclass THEN
  IF NEW.state IN('PREPARED','ACTIVE') OR NEW.state IS NOT DISTINCT FROM OLD.state THEN RETURN NULL; END IF;
  UPDATE control_plane.kubernetes_file_input_sources source
  SET state=CASE WHEN EXISTS(SELECT 1 FROM control_plane.kubernetes_input_stages stage
      WHERE stage.stream_id=source.stream_id AND stage.organization_id=source.organization_id AND stage.native_admitted_at IS NOT NULL)
     THEN 'UNKNOWN' ELSE 'CANCELLED' END,
   reason_code=CASE WHEN EXISTS(SELECT 1 FROM control_plane.kubernetes_input_stages stage
      WHERE stage.stream_id=source.stream_id AND stage.organization_id=source.organization_id AND stage.native_admitted_at IS NOT NULL)
     THEN 'INPUT_UNKNOWN' ELSE 'CANCELLED' END
  WHERE source.stream_id=NEW.id AND source.organization_id=NEW.organization_id AND source.payload_purged_at IS NULL AND source.state IN('PREPARING','READY','UNKNOWN');
 ELSE
  IF NEW.state IN('READY','RUNNING','WAITING_OWNER') OR NEW.state IS NOT DISTINCT FROM OLD.state THEN RETURN NULL; END IF;
  UPDATE control_plane.kubernetes_file_input_sources source SET state='CANCELLED',reason_code='CANCELLED'
  WHERE source.invocation_id=NEW.id AND source.organization_id=NEW.organization_id AND source.stream_id IS NULL
   AND source.payload_purged_at IS NULL AND source.state IN('PREPARING','READY','UNKNOWN');
 END IF;
 RETURN NULL;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.close_kubernetes_file_source() FROM PUBLIC;
CREATE TRIGGER kubernetes_file_stream_close AFTER UPDATE OF state ON control_plane.kubernetes_execution_streams
 FOR EACH ROW EXECUTE FUNCTION control_plane.close_kubernetes_file_source();
CREATE TRIGGER kubernetes_file_invocation_close AFTER UPDATE OF state ON control_plane.integration_invocations
 FOR EACH ROW EXECUTE FUNCTION control_plane.close_kubernetes_file_source();
REVOKE ALL ON control_plane.kubernetes_file_input_frames FROM control_plane_runtime;
GRANT SELECT,INSERT ON control_plane.kubernetes_file_input_frames TO control_plane_runtime;

-- FILE source только расширяет existing cleanup consumer; новая очередь либо retry engine не создаётся.
-- +goose StatementBegin
CREATE FUNCTION control_plane.kubernetes_file_source_cleanup_eligible(p_source uuid,p_org uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog AS $$
 SELECT EXISTS(SELECT 1 FROM control_plane.kubernetes_file_input_sources source
  JOIN control_plane.integration_invocations invocation ON invocation.id=source.invocation_id AND invocation.organization_id=source.organization_id
  LEFT JOIN control_plane.kubernetes_execution_streams stream ON stream.id=source.stream_id AND stream.organization_id=source.organization_id
  LEFT JOIN control_plane.prepared_content content ON content.organization_id=source.organization_id
   AND content.source_profile='KUBERNETES_FILE_SOURCE' AND content.source_profile_ref=source.ref
  WHERE source.id=p_source AND source.organization_id=p_org AND source.payload_purged_at IS NULL
   AND source.state IN('TRANSFERRED','REJECTED','CANCELLED','EXPIRED','UNKNOWN')
   AND(invocation.state IN('SUCCEEDED','FAILED','REJECTED','CANCELLED','UNKNOWN_OUTCOME')
    OR stream.state IN('COMPLETED','CANCELED','REVOKED','EXPIRED','LOST'))
   AND(source.writer_completed_at IS NOT NULL OR source.writer_deadline<=clock_timestamp())
   AND(content.id IS NULL AND source.prepared_content_id IS NULL OR content.source_profile='KUBERNETES_FILE_SOURCE' AND content.source_profile_ref=source.ref
    AND(source.prepared_content_id IS NULL OR source.prepared_content_id=content.id) AND content.actor_id=source.actor_id
    AND content.adopted_revision_id IS NULL AND content.adopted_revision_ref IS NULL
    AND content.deletion_receipt_digest IS NULL
    AND(content.state<>'IN_FLIGHT' OR content.writer_deadline<=clock_timestamp())
    AND(content.cleanup_deadline IS NULL OR content.cleanup_deadline<=clock_timestamp()
     OR content.cleanup_owner=source.cleanup_owner AND content.cleanup_deadline=source.cleanup_deadline)
    AND(content.adopted_file_source_id IS NULL OR content.adopted_file_source_id=source.id))
   AND NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_file_input_frames frame
    JOIN control_plane.kubernetes_input_stages stage ON stage.id=frame.stage_id AND stage.organization_id=frame.organization_id
    WHERE frame.source_id=source.id AND(stage.state IN('ATTEMPTING','QUEUED','CLAIMED')
     OR stage.delivery_expires_at>clock_timestamp())))
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.kubernetes_file_source_cleanup_eligible(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.kubernetes_file_source_cleanup_eligible(uuid,uuid) TO control_plane_runtime,artifact_retention_runtime;

-- Metadata FILE stages не являются S3 объектами: cleanup receipt разрешён только после source.
-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_kubernetes_file_stage_cleanup() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF NEW.channel<>'FILE_CONTENT' OR ROW(NEW.cleanup_generation,NEW.cleanup_owner,NEW.cleanup_deadline,NEW.cleanup_attempts,
  NEW.cleanup_total_attempts,NEW.cleanup_repair_generation,NEW.cleanup_object_version,NEW.cleanup_object_etag,NEW.payload_purged_at,NEW.deletion_receipt_digest)
  IS NOT DISTINCT FROM ROW(OLD.cleanup_generation,OLD.cleanup_owner,OLD.cleanup_deadline,OLD.cleanup_attempts,
  OLD.cleanup_total_attempts,OLD.cleanup_repair_generation,OLD.cleanup_object_version,OLD.cleanup_object_etag,OLD.payload_purged_at,OLD.deletion_receipt_digest) THEN RETURN NEW; END IF;
 IF current_user<>'control_plane_owner' OR OLD.payload_purged_at IS NOT NULL
  OR ROW(NEW.cleanup_generation,NEW.cleanup_owner,NEW.cleanup_deadline,NEW.cleanup_attempts,NEW.cleanup_total_attempts,
    NEW.cleanup_repair_generation,NEW.cleanup_object_version,NEW.cleanup_object_etag)
   IS DISTINCT FROM ROW(0::bigint,NULL::text,NULL::timestamptz,0,0::bigint,0::bigint,NULL::text,NULL::text)
  OR NEW.payload_purged_at IS NULL OR NEW.deletion_receipt_digest IS NULL
  OR NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_file_input_frames frame
   JOIN control_plane.kubernetes_file_input_sources source ON source.id=frame.source_id AND source.organization_id=frame.organization_id
   WHERE frame.stage_id=NEW.id AND frame.stream_id=NEW.stream_id AND frame.organization_id=NEW.organization_id
    AND source.payload_purged_at=NEW.payload_purged_at AND source.deletion_receipt_digest=NEW.deletion_receipt_digest
    AND source.cleanup_generation>0 AND source.cleanup_owner IS NULL AND source.cleanup_deadline IS NULL) THEN
  RAISE EXCEPTION 'Kubernetes FILE metadata cleanup lacks source receipt' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_kubernetes_file_stage_cleanup() FROM PUBLIC;
CREATE TRIGGER kubernetes_file_stage_cleanup_guard BEFORE UPDATE ON control_plane.kubernetes_input_stages
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_kubernetes_file_stage_cleanup();

-- FILE cleanup version принадлежит repair, а counters не обнуляют историю попыток.
-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_kubernetes_file_cleanup_transition() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF ROW(NEW.cleanup_generation,NEW.cleanup_owner,NEW.cleanup_deadline,NEW.cleanup_attempts,NEW.cleanup_total_attempts,
  NEW.cleanup_repair_generation,NEW.cleanup_version,NEW.cleanup_object_version,NEW.cleanup_object_etag,NEW.payload_purged_at,NEW.deletion_receipt_digest)
  IS NOT DISTINCT FROM ROW(OLD.cleanup_generation,OLD.cleanup_owner,OLD.cleanup_deadline,OLD.cleanup_attempts,OLD.cleanup_total_attempts,
  OLD.cleanup_repair_generation,OLD.cleanup_version,OLD.cleanup_object_version,OLD.cleanup_object_etag,OLD.payload_purged_at,OLD.deletion_receipt_digest) THEN RETURN NEW; END IF;
 IF current_user<>'control_plane_owner' OR OLD.payload_purged_at IS NOT NULL THEN
  RAISE EXCEPTION 'Kubernetes FILE cleanup transition is invalid' USING ERRCODE='23514'; END IF;
 IF NOT (
  OLD.cleanup_generation<9223372036854775807 AND OLD.cleanup_total_attempts<9223372036854775807
   AND OLD.cleanup_attempts<3 AND NEW.cleanup_generation=OLD.cleanup_generation+1
   AND control_plane.kubernetes_file_source_cleanup_eligible(OLD.id,OLD.organization_id)
   AND NEW.cleanup_attempts=OLD.cleanup_attempts+1 AND NEW.cleanup_total_attempts=OLD.cleanup_total_attempts+1
   AND NEW.cleanup_repair_generation=OLD.cleanup_repair_generation AND NEW.cleanup_version=OLD.cleanup_version
   AND(OLD.cleanup_deadline IS NULL OR OLD.cleanup_deadline<=clock_timestamp())
   AND((NEW.cleanup_owner IS NOT NULL AND octet_length(NEW.cleanup_owner) BETWEEN 1 AND 128
     AND NEW.cleanup_owner !~ '[[:space:][:cntrl:]]' AND NEW.cleanup_deadline>clock_timestamp()
     AND NEW.cleanup_deadline<=clock_timestamp()+interval '600 seconds' AND NEW.payload_purged_at IS NULL)
    OR(NEW.cleanup_owner IS NULL AND NEW.cleanup_deadline IS NULL AND NEW.payload_purged_at IS NOT NULL
      AND NEW.deletion_receipt_digest IS NOT NULL AND NEW.prepared_content_id IS NULL
      AND NEW.object_key IS NULL AND NOT EXISTS(SELECT 1 FROM control_plane.prepared_content content
       WHERE content.organization_id=OLD.organization_id AND content.source_profile='KUBERNETES_FILE_SOURCE' AND content.source_profile_ref=OLD.ref)))
  OR OLD.cleanup_attempts=3 AND NEW.cleanup_attempts=0 AND NEW.cleanup_total_attempts=OLD.cleanup_total_attempts
   AND OLD.cleanup_generation<9223372036854775807 AND NEW.cleanup_generation=OLD.cleanup_generation+1
   AND OLD.cleanup_repair_generation<9223372036854775807 AND NEW.cleanup_repair_generation=OLD.cleanup_repair_generation+1
   AND OLD.cleanup_version<9007199254740991 AND NEW.cleanup_version=OLD.cleanup_version+1
   AND NEW.cleanup_owner IS NULL AND NEW.cleanup_deadline IS NULL AND NEW.payload_purged_at IS NULL
   AND(OLD.cleanup_deadline IS NULL OR OLD.cleanup_deadline<=clock_timestamp())
   AND control_plane.kubernetes_file_source_cleanup_eligible(OLD.id,OLD.organization_id)
  OR NEW.cleanup_generation=OLD.cleanup_generation AND NEW.cleanup_attempts=OLD.cleanup_attempts
   AND NEW.cleanup_total_attempts=OLD.cleanup_total_attempts AND NEW.cleanup_repair_generation=OLD.cleanup_repair_generation
   AND NEW.cleanup_version=OLD.cleanup_version AND OLD.cleanup_generation>0
   AND OLD.cleanup_owner IS NOT NULL AND OLD.cleanup_deadline>clock_timestamp()
   AND(ROW(NEW.cleanup_owner,NEW.cleanup_deadline,NEW.payload_purged_at,NEW.deletion_receipt_digest)
      IS NOT DISTINCT FROM ROW(OLD.cleanup_owner,OLD.cleanup_deadline,OLD.payload_purged_at,OLD.deletion_receipt_digest)
    OR NEW.cleanup_owner IS NULL AND NEW.cleanup_deadline IS NULL)
 ) THEN RAISE EXCEPTION 'Kubernetes FILE cleanup counters are immutable' USING ERRCODE='23514'; END IF;
 IF OLD.cleanup_object_version IS NOT NULL AND ROW(NEW.cleanup_object_version,NEW.cleanup_object_etag)
  IS DISTINCT FROM ROW(OLD.cleanup_object_version,OLD.cleanup_object_etag) THEN
  RAISE EXCEPTION 'Kubernetes FILE cleanup version is immutable' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_kubernetes_file_cleanup_transition() FROM PUBLIC;
CREATE TRIGGER kubernetes_file_cleanup_transition BEFORE UPDATE ON control_plane.kubernetes_file_input_sources
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_kubernetes_file_cleanup_transition();

-- Только технический новый cycle: material version, source и native graph не меняются.
-- +goose StatementBegin
CREATE FUNCTION control_plane.repair_kubernetes_file_source_cleanup(p_org uuid,p_source uuid,p_expected bigint)
RETURNS TABLE(version bigint,repair_generation bigint,repaired_count integer,accepted_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE item control_plane.kubernetes_file_input_sources%ROWTYPE;
BEGIN
 IF NOT pg_has_role(session_user,'control_plane_runtime','MEMBER') OR NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=session_user AND rolcanlogin) THEN
  RAISE EXCEPTION 'Kubernetes FILE cleanup repair caller is invalid'; END IF;
 SELECT source.* INTO item FROM control_plane.kubernetes_file_input_sources source
 WHERE source.id=p_source AND source.organization_id=p_org AND source.cleanup_version=p_expected
  AND source.cleanup_version<9007199254740991 AND source.cleanup_repair_generation<9223372036854775807
  AND source.cleanup_generation<9223372036854775807 AND source.cleanup_attempts=3
  AND(source.cleanup_deadline IS NULL OR source.cleanup_deadline<=clock_timestamp()) FOR UPDATE;
 IF NOT FOUND OR NOT control_plane.kubernetes_file_source_cleanup_eligible(item.id,item.organization_id) THEN RETURN; END IF;
 RETURN QUERY UPDATE control_plane.kubernetes_file_input_sources source SET cleanup_attempts=0,
  cleanup_repair_generation=source.cleanup_repair_generation+1,cleanup_version=source.cleanup_version+1,
  cleanup_generation=source.cleanup_generation+1,cleanup_owner=NULL,cleanup_deadline=NULL
 WHERE source.id=item.id AND source.organization_id=item.organization_id
 RETURNING source.cleanup_version,source.cleanup_repair_generation,1,clock_timestamp();
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.repair_kubernetes_file_source_cleanup(uuid,uuid,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.repair_kubernetes_file_source_cleanup(uuid,uuid,bigint) TO control_plane_runtime;

-- Один immutable DELETE admission на normalized source. Repair меняет только цикл
-- HEAD reconciliation; прежний original generation/owner/deadline сохраняется.
CREATE TABLE control_plane.kubernetes_file_delete_admissions (
 source_id uuid NOT NULL,
 organization_id uuid NOT NULL,
 object_key text NOT NULL CHECK(octet_length(object_key) BETWEEN 1 AND 1024),
 object_version text NOT NULL CHECK(octet_length(object_version) BETWEEN 1 AND 1024 AND lower(object_version)<>'null'),
 object_etag text NOT NULL CHECK(octet_length(object_etag) BETWEEN 1 AND 1024),
 digest text NOT NULL CHECK(digest ~ '^sha256:[a-f0-9]{64}$'),
 size_bytes bigint NOT NULL CHECK(size_bytes BETWEEN 1 AND 16777216),
 cleanup_generation bigint NOT NULL CHECK(cleanup_generation>0),
 cleanup_repair_generation bigint NOT NULL CHECK(cleanup_repair_generation>=0),
 cleanup_owner text NOT NULL CHECK(octet_length(cleanup_owner) BETWEEN 1 AND 128 AND cleanup_owner !~ '[[:space:][:cntrl:]]'),
 cleanup_deadline timestamptz NOT NULL,
 admitted_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(source_id,organization_id),
 FOREIGN KEY(source_id,organization_id) REFERENCES control_plane.kubernetes_file_input_sources(id,organization_id) DEFERRABLE INITIALLY IMMEDIATE,
 CHECK(cleanup_deadline>admitted_at)
);
-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_kubernetes_file_delete_admission() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF TG_OP='DELETE' THEN
  IF current_user='control_plane_owner' AND EXISTS(SELECT 1 FROM control_plane.kubernetes_file_input_sources source
   JOIN control_plane.project_purge_context context ON context.project_id=source.project_id
   WHERE source.id=OLD.source_id AND source.organization_id=OLD.organization_id AND source.payload_purged_at IS NOT NULL
    AND source.deletion_receipt_digest IS NOT NULL AND context.transaction_id=pg_current_xact_id_if_assigned() AND context.backend_pid=pg_backend_pid()) THEN RETURN OLD; END IF;
  RAISE EXCEPTION 'Kubernetes FILE delete admission is retained' USING ERRCODE='23514';
 END IF;
 IF TG_OP<>'INSERT' OR current_user<>'control_plane_owner' OR NOT EXISTS(
  SELECT 1 FROM control_plane.kubernetes_file_input_sources source
  JOIN control_plane.prepared_content content ON content.organization_id=source.organization_id
   AND content.source_profile='KUBERNETES_FILE_SOURCE' AND content.source_profile_ref=source.ref
  WHERE source.id=NEW.source_id AND source.organization_id=NEW.organization_id AND source.payload_purged_at IS NULL
   AND source.cleanup_generation=NEW.cleanup_generation AND source.cleanup_repair_generation=NEW.cleanup_repair_generation
   AND source.cleanup_owner=NEW.cleanup_owner AND source.cleanup_deadline=NEW.cleanup_deadline AND source.cleanup_deadline>clock_timestamp()
   AND source.cleanup_object_version=NEW.object_version AND source.cleanup_object_etag=NEW.object_etag
   AND content.object_key=NEW.object_key AND content.object_version=NEW.object_version AND content.object_etag=NEW.object_etag
   AND content.digest=NEW.digest AND content.size_bytes=NEW.size_bytes AND content.deletion_receipt_digest IS NULL
   AND control_plane.kubernetes_file_source_cleanup_eligible(source.id,source.organization_id)) THEN
  RAISE EXCEPTION 'Kubernetes FILE delete admission mismatch' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_kubernetes_file_delete_admission() FROM PUBLIC;
CREATE TRIGGER kubernetes_file_delete_admission_guard BEFORE INSERT OR UPDATE OR DELETE ON control_plane.kubernetes_file_delete_admissions
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_kubernetes_file_delete_admission();
REVOKE ALL ON control_plane.kubernetes_file_delete_admissions FROM PUBLIC,control_plane_runtime,artifact_retention_runtime;

-- FILE использует тот же retention consumer и exact source mutex, что reservation.
-- Helper закрыт от runtime: выдаёт только locked owner row, не новое право на bytes.
-- +goose StatementBegin
CREATE FUNCTION control_plane.lock_kubernetes_file_cleanup(p_id uuid)
RETURNS control_plane.kubernetes_file_input_sources
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE snapshot control_plane.kubernetes_file_input_sources%ROWTYPE;
 item control_plane.kubernetes_file_input_sources%ROWTYPE; prepared_id uuid;
BEGIN
 SELECT source.* INTO snapshot FROM control_plane.kubernetes_file_input_sources source WHERE source.id=p_id;
 IF NOT FOUND THEN RETURN NULL; END IF;
 IF snapshot.stream_id IS NOT NULL THEN
  PERFORM 1 FROM control_plane.kubernetes_execution_streams stream
   WHERE stream.id=snapshot.stream_id AND stream.organization_id=snapshot.organization_id FOR UPDATE SKIP LOCKED;
  IF NOT FOUND THEN RETURN NULL; END IF;
 END IF;
 SELECT source.* INTO item FROM control_plane.kubernetes_file_input_sources source
  WHERE source.id=snapshot.id AND source.organization_id=snapshot.organization_id FOR UPDATE SKIP LOCKED;
 IF NOT FOUND OR item.stream_id IS DISTINCT FROM snapshot.stream_id THEN RETURN NULL; END IF;
 IF NOT pg_try_advisory_xact_lock(hashtextextended('kodex.kubernetes.file.source:'||item.organization_id::text||':'||item.ref,0)) THEN RETURN NULL; END IF;
 SELECT content.id INTO prepared_id FROM control_plane.prepared_content content
  WHERE content.organization_id=item.organization_id AND content.source_profile='KUBERNETES_FILE_SOURCE' AND content.source_profile_ref=item.ref;
 IF prepared_id IS NOT NULL THEN
  PERFORM 1 FROM control_plane.prepared_content content WHERE content.id=prepared_id FOR UPDATE SKIP LOCKED;
  IF NOT FOUND THEN RETURN NULL; END IF;
 END IF;
 IF NOT control_plane.kubernetes_file_source_cleanup_eligible(item.id,item.organization_id) THEN RETURN NULL; END IF;
 RETURN item;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.lock_kubernetes_file_cleanup(uuid) FROM PUBLIC,control_plane_runtime,artifact_retention_runtime;

-- Количество обработанных source ограничено p_limit, включая отсутствие object intent.
-- NO_OBJECT_INTENT доказывается под mutex после writer fence, а не по HEAD 404.
-- +goose StatementBegin
CREATE FUNCTION control_plane.kubernetes_file_source_cleanup_claim(p_owner text,p_limit integer,p_lease integer)
RETURNS TABLE(source_kind text,ledger_id uuid,object_key text,object_version text,object_etag text,digest text,size_bytes bigint,generation bigint,uncertain boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE candidate record; item control_plane.kubernetes_file_input_sources%ROWTYPE;
 content control_plane.prepared_content%ROWTYPE; processed integer:=0; deadline timestamptz; admitted uuid;
BEGIN
 IF NOT pg_has_role(session_user,'artifact_retention_runtime','MEMBER') OR NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=session_user AND rolcanlogin)
  OR p_owner IS NULL OR octet_length(p_owner) NOT BETWEEN 1 AND 128 OR p_owner ~ '[[:space:][:cntrl:]]'
  OR p_limit IS NULL OR p_limit NOT BETWEEN 1 AND 100 OR p_lease IS NULL OR p_lease NOT BETWEEN 1 AND 600 THEN
  RAISE EXCEPTION 'Kubernetes FILE cleanup claim is invalid'; END IF;
 FOR candidate IN SELECT source.id FROM control_plane.kubernetes_file_input_sources source
  WHERE source.payload_purged_at IS NULL AND source.cleanup_attempts<3
   AND source.cleanup_generation<9223372036854775807 AND source.cleanup_total_attempts<9223372036854775807
   AND NOT EXISTS(SELECT 1 FROM control_plane.prepared_content prepared WHERE prepared.organization_id=source.organization_id
    AND prepared.source_profile='KUBERNETES_FILE_SOURCE' AND prepared.source_profile_ref=source.ref AND prepared.state<>'ADOPTED' AND prepared.generation=9223372036854775807)
   AND(source.cleanup_deadline IS NULL OR source.cleanup_deadline<=clock_timestamp())
   AND control_plane.kubernetes_file_source_cleanup_eligible(source.id,source.organization_id)
  ORDER BY source.created_at,source.id LIMIT p_limit*4
 LOOP
  EXIT WHEN processed>=p_limit;
  item:=control_plane.lock_kubernetes_file_cleanup(candidate.id);
  IF item.id IS NULL OR item.cleanup_attempts>=3 OR item.cleanup_generation=9223372036854775807
   OR item.cleanup_total_attempts=9223372036854775807 OR item.cleanup_deadline>clock_timestamp() THEN CONTINUE; END IF;
  SELECT prepared.* INTO content FROM control_plane.prepared_content prepared
   WHERE prepared.organization_id=item.organization_id AND prepared.source_profile='KUBERNETES_FILE_SOURCE' AND prepared.source_profile_ref=item.ref;
  IF NOT FOUND THEN
   IF item.prepared_content_id IS NOT NULL OR item.object_key IS NOT NULL THEN CONTINUE; END IF;
   UPDATE control_plane.kubernetes_file_input_sources source SET cleanup_generation=source.cleanup_generation+1,
    cleanup_attempts=source.cleanup_attempts+1,cleanup_total_attempts=source.cleanup_total_attempts+1,
    payload_purged_at=clock_timestamp(),deletion_receipt_digest=encode(public.digest(convert_to(jsonb_build_object(
     'source',source.id,'kind','KUBERNETES_FILE_SOURCE','generation',source.cleanup_generation+1,'proof','NO_OBJECT_INTENT')::text,'UTF8'),'sha256'),'hex')
   WHERE source.id=item.id;
   processed:=processed+1;
   CONTINUE;
  END IF;
  IF content.deletion_receipt_digest IS NOT NULL OR content.state<>'ADOPTED' AND content.generation=9223372036854775807
   OR content.cleanup_deadline>clock_timestamp() OR content.object_key IS NULL OR content.digest IS NULL OR content.size_bytes IS NULL THEN CONTINUE; END IF;
  deadline:=clock_timestamp()+p_lease*interval '1 second';
  UPDATE control_plane.kubernetes_file_input_sources source SET cleanup_owner=p_owner,cleanup_deadline=deadline,
   cleanup_attempts=source.cleanup_attempts+1,cleanup_total_attempts=source.cleanup_total_attempts+1,cleanup_generation=source.cleanup_generation+1,
   cleanup_object_version=coalesce(source.cleanup_object_version,source.object_version,nullif(content.object_version,'')),
   cleanup_object_etag=coalesce(source.cleanup_object_etag,source.object_etag,nullif(content.object_etag,''))
  WHERE source.id=item.id;
  IF content.state<>'ADOPTED' THEN
   UPDATE control_plane.prepared_content prepared SET state='CLEANING',cleanup_owner=p_owner,cleanup_deadline=deadline,
    cleanup_attempts=item.cleanup_attempts+1,generation=prepared.generation+1,updated_at=clock_timestamp()
   WHERE prepared.id=content.id;
  END IF;
  source_kind:='KUBERNETES_FILE_SOURCE';ledger_id:=item.id;object_key:=content.object_key;
  object_version:=coalesce(item.cleanup_object_version,item.object_version,content.object_version,'');
  object_etag:=coalesce(item.cleanup_object_etag,item.object_etag,content.object_etag,'');
  digest:=content.digest;size_bytes:=content.size_bytes;generation:=item.cleanup_generation+1;
  admitted:=NULL;
  IF object_version<>'' THEN
   INSERT INTO control_plane.kubernetes_file_delete_admissions(source_id,organization_id,object_key,object_version,object_etag,digest,size_bytes,
    cleanup_generation,cleanup_repair_generation,cleanup_owner,cleanup_deadline)
   VALUES(item.id,item.organization_id,content.object_key,
    coalesce(item.cleanup_object_version,item.object_version,content.object_version,''),
    coalesce(item.cleanup_object_etag,item.object_etag,content.object_etag,''),content.digest,content.size_bytes,
    item.cleanup_generation+1,item.cleanup_repair_generation,p_owner,deadline)
   ON CONFLICT DO NOTHING RETURNING source_id INTO admitted;
  END IF;
  -- Known receipt + uncertain=true означает HEAD_ONLY, даже после explicit repair.
  uncertain:=admitted IS NULL;processed:=processed+1;RETURN NEXT;
 END LOOP;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.kubernetes_file_source_cleanup_claim(text,integer,integer) FROM PUBLIC,control_plane_runtime;
GRANT EXECUTE ON FUNCTION control_plane.kubernetes_file_source_cleanup_claim(text,integer,integer) TO artifact_retention_runtime;

-- HEAD receipt должен совпадать с исходным unique prepared intent; 404 не подтверждает отсутствие.
-- +goose StatementBegin
CREATE FUNCTION control_plane.kubernetes_file_source_cleanup_record(p_id uuid,p_owner text,p_generation bigint,p_key text,p_version text,p_etag text,p_digest text,p_size bigint)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE item control_plane.kubernetes_file_input_sources%ROWTYPE; content control_plane.prepared_content%ROWTYPE; admitted uuid;
BEGIN
 IF NOT pg_has_role(session_user,'artifact_retention_runtime','MEMBER') OR NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=session_user AND rolcanlogin)
  OR p_version IS NULL OR p_version='' OR lower(p_version)='null' OR p_etag IS NULL OR p_etag='' THEN RETURN false; END IF;
 item:=control_plane.lock_kubernetes_file_cleanup(p_id);
 IF item.id IS NULL OR item.cleanup_owner IS DISTINCT FROM p_owner OR item.cleanup_generation IS DISTINCT FROM p_generation
  OR item.cleanup_deadline IS NULL OR item.cleanup_deadline<=clock_timestamp() THEN RETURN false; END IF;
 SELECT prepared.* INTO content FROM control_plane.prepared_content prepared
  WHERE prepared.organization_id=item.organization_id AND prepared.source_profile='KUBERNETES_FILE_SOURCE' AND prepared.source_profile_ref=item.ref;
 IF NOT FOUND OR content.object_key IS DISTINCT FROM p_key OR content.digest IS DISTINCT FROM p_digest OR content.size_bytes IS DISTINCT FROM p_size
  OR content.deletion_receipt_digest IS NOT NULL
  OR content.object_version<>'' AND ROW(content.object_version,content.object_etag) IS DISTINCT FROM ROW(p_version,p_etag)
  OR item.cleanup_object_version IS NOT NULL AND ROW(item.cleanup_object_version,item.cleanup_object_etag) IS DISTINCT FROM ROW(p_version,p_etag) THEN RETURN false; END IF;
 IF content.state<>'ADOPTED' THEN
  IF content.state<>'CLEANING' OR content.cleanup_owner IS DISTINCT FROM p_owner OR content.cleanup_deadline IS DISTINCT FROM item.cleanup_deadline THEN RETURN false; END IF;
  UPDATE control_plane.prepared_content prepared SET object_version=p_version,object_etag=p_etag,updated_at=clock_timestamp() WHERE prepared.id=content.id;
 END IF;
 UPDATE control_plane.kubernetes_file_input_sources source SET cleanup_object_version=p_version,cleanup_object_etag=p_etag WHERE source.id=item.id;
 INSERT INTO control_plane.kubernetes_file_delete_admissions(source_id,organization_id,object_key,object_version,object_etag,digest,size_bytes,
  cleanup_generation,cleanup_repair_generation,cleanup_owner,cleanup_deadline)
 VALUES(item.id,item.organization_id,p_key,p_version,p_etag,p_digest,p_size,item.cleanup_generation,item.cleanup_repair_generation,p_owner,item.cleanup_deadline)
 ON CONFLICT DO NOTHING RETURNING source_id INTO admitted;
 -- False не разрешает DELETE. Потерянный Record ACK не выдаёт этот admission повторно.
 RETURN admitted IS NOT NULL;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.kubernetes_file_source_cleanup_record(uuid,text,bigint,text,text,text,text,bigint) FROM PUBLIC,control_plane_runtime;
GRANT EXECUTE ON FUNCTION control_plane.kubernetes_file_source_cleanup_record(uuid,text,bigint,text,text,text,text,bigint) TO artifact_retention_runtime;

-- Один exact DELETE receipt закрывает source и все metadata-only stages в одной транзакции.
-- Native state/ACK/sequence и origin FK не меняются; origin release выполняет canonical purge отдельно.
-- +goose StatementBegin
CREATE FUNCTION control_plane.kubernetes_file_source_cleanup_complete(p_id uuid,p_owner text,p_generation bigint,p_version text,p_etag text,p_success boolean)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE item control_plane.kubernetes_file_input_sources%ROWTYPE; content control_plane.prepared_content%ROWTYPE; proof text; purged timestamptz;
BEGIN
 IF NOT pg_has_role(session_user,'artifact_retention_runtime','MEMBER') OR NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=session_user AND rolcanlogin)
  OR p_success IS NULL THEN RETURN false; END IF;
 item:=control_plane.lock_kubernetes_file_cleanup(p_id);
 IF item.id IS NULL OR item.cleanup_owner IS DISTINCT FROM p_owner OR item.cleanup_generation IS DISTINCT FROM p_generation
  OR item.cleanup_deadline IS NULL OR item.cleanup_deadline<=clock_timestamp() THEN RETURN false; END IF;
 SELECT prepared.* INTO content FROM control_plane.prepared_content prepared
  WHERE prepared.organization_id=item.organization_id AND prepared.source_profile='KUBERNETES_FILE_SOURCE' AND prepared.source_profile_ref=item.ref;
 IF NOT FOUND OR content.deletion_receipt_digest IS NOT NULL OR content.state<>'ADOPTED' AND(content.state<>'CLEANING'
  OR content.cleanup_owner IS DISTINCT FROM p_owner OR content.cleanup_deadline IS DISTINCT FROM item.cleanup_deadline) THEN RETURN false; END IF;
 IF p_success THEN
  IF p_version IS NULL OR p_version='' OR lower(p_version)='null' OR p_etag IS NULL OR p_etag=''
   OR item.cleanup_object_version IS DISTINCT FROM p_version OR item.cleanup_object_etag IS DISTINCT FROM p_etag
   OR content.object_version IS DISTINCT FROM p_version OR content.object_etag IS DISTINCT FROM p_etag
   OR NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_file_delete_admissions admission
    WHERE admission.source_id=item.id AND admission.organization_id=item.organization_id AND admission.object_key=content.object_key
     AND admission.object_version=p_version AND admission.object_etag=p_etag AND admission.digest=content.digest AND admission.size_bytes=content.size_bytes)
   THEN RETURN false; END IF;
  purged:=clock_timestamp();
  proof:=encode(public.digest(convert_to(jsonb_build_object('source',item.id,'kind','KUBERNETES_FILE_SOURCE',
   'generation',item.cleanup_generation,'version',p_version,'etag',p_etag)::text,'UTF8'),'sha256'),'hex');
  UPDATE control_plane.kubernetes_file_input_sources source SET payload_purged_at=purged,deletion_receipt_digest=proof,
   cleanup_owner=NULL,cleanup_deadline=NULL WHERE source.id=item.id;
  UPDATE control_plane.kubernetes_input_stages stage SET payload_purged_at=purged,deletion_receipt_digest=proof
   FROM control_plane.kubernetes_file_input_frames frame WHERE frame.source_id=item.id AND frame.organization_id=item.organization_id
    AND stage.id=frame.stage_id AND stage.organization_id=frame.organization_id AND stage.payload_purged_at IS NULL;
  UPDATE control_plane.prepared_content prepared SET state=CASE WHEN prepared.state='ADOPTED' THEN 'ADOPTED' ELSE 'CLEANED' END,
   ref=NULL,file_name=NULL,media_type=NULL,digest=NULL,size_bytes=NULL,scan_state=NULL,preview_state=NULL,
   object_key=NULL,object_version=NULL,object_etag=NULL,deletion_receipt_digest=proof,cleanup_owner=NULL,cleanup_deadline=NULL,updated_at=purged
  WHERE prepared.id=content.id;
 ELSE
  IF content.state='CLEANING' THEN
   UPDATE control_plane.prepared_content prepared SET state=CASE WHEN item.cleanup_attempts=3 THEN 'WAITING_OWNER' ELSE 'ABANDONED' END,
    cleanup_owner=NULL,cleanup_deadline=NULL,updated_at=clock_timestamp() WHERE prepared.id=content.id;
  END IF;
  UPDATE control_plane.kubernetes_file_input_sources source SET cleanup_owner=NULL,cleanup_deadline=NULL WHERE source.id=item.id;
 END IF;
 RETURN true;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.kubernetes_file_source_cleanup_complete(uuid,text,bigint,text,text,boolean) FROM PUBLIC,control_plane_runtime;
GRANT EXECUTE ON FUNCTION control_plane.kubernetes_file_source_cleanup_complete(uuid,text,bigint,text,text,boolean) TO artifact_retention_runtime;

-- Canonical artifact retention использует этот hook до physical DELETE.
-- До typed terminal release исходный FK остаётся authoritative retention edge.
-- +goose StatementBegin
CREATE FUNCTION control_plane.kubernetes_file_origin_retained(p_artifact uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog AS $$
 SELECT EXISTS(SELECT 1 FROM control_plane.kubernetes_file_input_origins origin
  WHERE origin.artifact_id=p_artifact AND origin.artifact_revision_id IS NOT NULL);
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.kubernetes_file_origin_retained(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.kubernetes_file_origin_retained(uuid) TO control_plane_runtime,artifact_retention_runtime;

-- Caller берёт invocation -> sources -> origin locks. Predicate не выдаёт право на DELETE,
-- не заменяет exact project/org scope и не утверждает remote join после UNKNOWN.
-- +goose StatementBegin
CREATE FUNCTION control_plane.kubernetes_file_origin_release_eligible(p_invocation uuid,p_org uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog AS $$
 SELECT EXISTS(SELECT 1 FROM control_plane.kubernetes_file_input_origins origin
  JOIN control_plane.integration_invocations invocation ON invocation.id=origin.invocation_id AND invocation.organization_id=origin.organization_id
  WHERE origin.invocation_id=p_invocation AND origin.organization_id=p_org AND origin.artifact_revision_id IS NOT NULL
   AND invocation.state IN('SUCCEEDED','FAILED','REJECTED','CANCELLED','UNKNOWN_OUTCOME')
   AND NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_file_input_sources source
    WHERE source.invocation_id=origin.invocation_id AND source.organization_id=origin.organization_id
     AND(source.payload_purged_at IS NULL OR source.deletion_receipt_digest IS NULL
      OR source.writer_completed_at IS NULL AND source.writer_deadline>clock_timestamp()
      OR source.cleanup_owner IS NOT NULL OR source.cleanup_deadline IS NOT NULL))
   AND NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_file_input_sources source
    JOIN control_plane.kubernetes_file_input_frames frame ON frame.source_id=source.id AND frame.organization_id=source.organization_id
    JOIN control_plane.kubernetes_input_stages stage ON stage.id=frame.stage_id AND stage.organization_id=frame.organization_id
    WHERE source.invocation_id=origin.invocation_id AND source.organization_id=origin.organization_id
     AND(stage.state IN('ATTEMPTING','QUEUED','CLAIMED') OR stage.delivery_expires_at>clock_timestamp())));
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.kubernetes_file_origin_release_eligible(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.kubernetes_file_origin_release_eligible(uuid,uuid) TO control_plane_runtime,artifact_retention_runtime;

-- Неизвестный physical object блокирует потерю original-root read/repair authority.
-- Функция возвращает только count; object locators и bytes не входят в public recovery.
-- +goose StatementBegin
CREATE FUNCTION control_plane.kubernetes_project_unreconciled_count(p_org uuid,p_project uuid)
RETURNS bigint LANGUAGE plpgsql SECURITY DEFINER VOLATILE SET search_path=pg_catalog AS $$
DECLARE result bigint; file_count bigint:=0;
BEGIN
 IF p_org IS NULL OR p_project IS NULL THEN RAISE EXCEPTION 'Kubernetes project boundary is invalid'; END IF;
 WITH streams AS MATERIALIZED (
  SELECT stream.id FROM control_plane.kubernetes_execution_streams stream
  JOIN control_plane.kubernetes_execution_effects effect ON effect.id=stream.effect_id AND effect.organization_id=stream.organization_id
  JOIN control_plane.kubernetes_execution_children child ON child.id=effect.child_id AND child.organization_id=effect.organization_id
  JOIN control_plane.kubernetes_execution_plans plan ON plan.id=child.plan_id AND plan.organization_id=child.organization_id
  JOIN control_plane.kubernetes_invocation_identity_bindings pin ON pin.invocation_id=plan.invocation_id AND pin.organization_id=plan.organization_id
  JOIN control_plane.runs root ON root.id=pin.root_run_id AND root.organization_id=pin.organization_id
  WHERE stream.organization_id=p_org AND root.project_id=p_project
 ), pending AS (
  SELECT stage.id FROM control_plane.kubernetes_input_stages stage JOIN streams ON streams.id=stage.stream_id
  WHERE stage.organization_id=p_org AND stage.channel<>'FILE_CONTENT' AND stage.payload_purged_at IS NULL
   AND(coalesce(nullif(stage.cleanup_object_version,''),nullif(stage.object_version,'')) IS NULL
    OR coalesce(nullif(stage.cleanup_object_etag,''),nullif(stage.object_etag,'')) IS NULL)
  UNION ALL
  SELECT stage.id FROM control_plane.kubernetes_output_stages stage JOIN streams ON streams.id=stage.stream_id
  WHERE stage.organization_id=p_org AND stage.payload_purged_at IS NULL
   AND(coalesce(nullif(stage.cleanup_object_version,''),nullif(stage.object_version,'')) IS NULL
    OR coalesce(nullif(stage.cleanup_object_etag,''),nullif(stage.object_etag,'')) IS NULL)
  UNION ALL
  SELECT prepared.id FROM control_plane.prepared_content prepared
  WHERE prepared.organization_id=p_org AND prepared.project_id=p_project AND prepared.source_profile='KUBERNETES_OUTPUT'
   AND prepared.state<>'CLEANED' AND prepared.deletion_receipt_digest IS NULL
   AND(nullif(prepared.object_version,'') IS NULL
    OR nullif(prepared.object_etag,'') IS NULL)
 ) SELECT count(*) INTO result FROM pending;
 -- FILE purpose schema имеет отдельный immutable source до появления prepared intent.
 -- Одна source считается один раз, включая случай, когда receipt известен только prepared.
 -- Даже known FILE receipt нельзя передавать generic project DELETE: source хранит
 -- durable admission и остаётся доступным для exact reconciliation до Trash.
 SELECT count(*) INTO file_count FROM control_plane.kubernetes_file_input_sources source
 WHERE source.organization_id=p_org AND source.project_id=p_project AND source.payload_purged_at IS NULL;
 RETURN result+file_count;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.kubernetes_project_unreconciled_count(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.kubernetes_project_unreconciled_count(uuid,uuid) TO control_plane_runtime,artifact_retention_runtime;


-- Единый exact-version receipt rejoin используется inventory и effect consumer.
-- FILE physical object принадлежит prepared/source; FILE INPUT frames — только metadata.
-- +goose StatementBegin
CREATE FUNCTION control_plane.kubernetes_project_object_receipts(p_org uuid,p_project uuid)
RETURNS TABLE(object_key text,object_version text,object_etag text,digest text,size_bytes bigint)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
 WITH streams AS MATERIALIZED (
  SELECT stream.id FROM control_plane.kubernetes_execution_streams stream
  JOIN control_plane.kubernetes_execution_effects effect ON effect.id=stream.effect_id AND effect.organization_id=stream.organization_id
  JOIN control_plane.kubernetes_execution_children child ON child.id=effect.child_id AND child.organization_id=effect.organization_id
  JOIN control_plane.kubernetes_execution_plans plan ON plan.id=child.plan_id AND plan.organization_id=child.organization_id
  JOIN control_plane.kubernetes_invocation_identity_bindings pin ON pin.invocation_id=plan.invocation_id AND pin.organization_id=plan.organization_id
  JOIN control_plane.runs root ON root.id=pin.root_run_id AND root.organization_id=pin.organization_id
  WHERE stream.organization_id=p_org AND root.project_id=p_project
 )
 SELECT stage.object_key,coalesce(stage.cleanup_object_version,stage.object_version),
  coalesce(stage.cleanup_object_etag,stage.object_etag),'sha256:'||stage.payload_digest,stage.byte_count::bigint
 FROM control_plane.kubernetes_input_stages stage JOIN streams ON streams.id=stage.stream_id
 WHERE stage.organization_id=p_org AND stage.channel<>'FILE_CONTENT' AND stage.payload_purged_at IS NULL
  AND coalesce(nullif(stage.cleanup_object_version,''),nullif(stage.object_version,'')) IS NOT NULL
  AND coalesce(nullif(stage.cleanup_object_etag,''),nullif(stage.object_etag,'')) IS NOT NULL
 UNION
 SELECT stage.object_key,coalesce(stage.cleanup_object_version,stage.object_version),
  coalesce(stage.cleanup_object_etag,stage.object_etag),'sha256:'||stage.payload_digest,stage.byte_count::bigint
 FROM control_plane.kubernetes_output_stages stage JOIN streams ON streams.id=stage.stream_id
 WHERE stage.organization_id=p_org AND stage.payload_purged_at IS NULL
  AND coalesce(nullif(stage.cleanup_object_version,''),nullif(stage.object_version,'')) IS NOT NULL
  AND coalesce(nullif(stage.cleanup_object_etag,''),nullif(stage.object_etag,'')) IS NOT NULL
 UNION
 SELECT prepared.object_key,nullif(prepared.object_version,''),
  nullif(prepared.object_etag,''),prepared.digest,prepared.size_bytes
 FROM control_plane.prepared_content prepared
 WHERE prepared.organization_id=p_org AND prepared.project_id=p_project
  AND prepared.source_profile='KUBERNETES_OUTPUT'
  AND prepared.state<>'CLEANED' AND prepared.deletion_receipt_digest IS NULL
  AND prepared.object_key IS NOT NULL AND prepared.digest IS NOT NULL AND prepared.size_bytes IS NOT NULL
  AND nullif(prepared.object_version,'') IS NOT NULL
  AND nullif(prepared.object_etag,'') IS NOT NULL;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.kubernetes_project_object_receipts(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.kubernetes_project_object_receipts(uuid,uuid) TO control_plane_runtime;

-- Дополнение existing canonical inventory, без изменения прежних artifact/avatar/archive/PVC readers.
-- +goose StatementBegin
DO $kubernetes_project_inventory$
DECLARE definition text; marker text:='WHERE session.organization_id = p_organization_id AND session.project_id = p_project_id;';
BEGIN
 SELECT pg_get_functiondef('control_plane.project_purge_external_inventory(uuid,uuid)'::regprocedure) INTO definition;
 IF(length(definition)-length(replace(definition,marker,'')))/length(marker)<>1 THEN
  RAISE EXCEPTION 'Kubernetes project inventory predecessor is unexpected'; END IF;
 EXECUTE replace(definition,marker,'WHERE session.organization_id = p_organization_id AND session.project_id = p_project_id
 UNION SELECT ''OBJECT'',receipt.object_key,receipt.object_version
 FROM control_plane.kubernetes_project_object_receipts(p_organization_id,p_project_id) receipt;');
END $kubernetes_project_inventory$;
-- +goose StatementEnd


-- Known, fenced OUTPUT prepared может быть удалён canonical project purge без публикации.
-- UNKNOWN остаётся отдельным preTrash blocker; FILE подключает свой typed source proof.
-- +goose StatementBegin
CREATE FUNCTION control_plane.kubernetes_prepared_project_purge_eligible(p_id uuid,p_org uuid,p_project uuid)
RETURNS boolean LANGUAGE sql VOLATILE SET search_path=pg_catalog AS $$
 SELECT EXISTS(SELECT 1 FROM control_plane.prepared_content prepared
 JOIN control_plane.projects project ON project.id=prepared.project_id AND project.organization_id=prepared.organization_id
 JOIN control_plane.kubernetes_output_captures capture ON capture.ref=prepared.source_profile_ref AND capture.organization_id=prepared.organization_id
 WHERE prepared.id=p_id AND prepared.organization_id=p_org AND prepared.project_id=p_project
  AND project.lifecycle='PURGE_PENDING' AND prepared.source_profile='KUBERNETES_OUTPUT'
  AND prepared.state NOT IN('ADOPTED','CLEANED','OWNER_ABANDONED') AND prepared.adopted_revision_id IS NULL
  AND prepared.deletion_receipt_digest IS NULL AND prepared.object_key IS NOT NULL
  AND prepared.digest IS NOT NULL AND prepared.size_bytes IS NOT NULL
  AND nullif(prepared.object_version,'') IS NOT NULL
  AND nullif(prepared.object_etag,'') IS NOT NULL
  AND prepared.writer_deadline<=clock_timestamp()
  AND(prepared.cleanup_deadline IS NULL OR prepared.cleanup_deadline<=clock_timestamp())
  AND NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_capture_attempts attempt
   WHERE attempt.capture_id=capture.id AND attempt.organization_id=capture.organization_id
    AND attempt.state='ACTIVE' AND attempt.expires_at>clock_timestamp()));
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.kubernetes_prepared_project_purge_eligible(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.kubernetes_prepared_project_purge_eligible(uuid,uuid,uuid) TO control_plane_runtime;

-- +goose StatementBegin
CREATE FUNCTION control_plane.kubernetes_project_prepared_scrub_valid(old_row control_plane.prepared_content,new_row control_plane.prepared_content)
RETURNS boolean LANGUAGE sql VOLATILE SET search_path=pg_catalog AS $$
 SELECT current_user='control_plane_owner' AND (old_row).source_profile='KUBERNETES_OUTPUT'
  AND control_plane.kubernetes_prepared_project_purge_eligible((old_row).id,(old_row).organization_id,(old_row).project_id)
  AND (new_row).state='CLEANED' AND (new_row).adopted_revision_id IS NULL
  AND (new_row).ref IS NULL AND (new_row).file_name IS NULL AND (new_row).media_type IS NULL
  AND (new_row).digest IS NULL AND (new_row).size_bytes IS NULL AND (new_row).scan_state IS NULL AND (new_row).preview_state IS NULL
  AND (new_row).object_key IS NULL AND (new_row).object_version IS NULL AND (new_row).object_etag IS NULL
  AND (new_row).cleanup_owner IS NULL AND (new_row).cleanup_deadline IS NULL
  AND(to_jsonb(old_row)-ARRAY['ref','file_name','media_type','digest','size_bytes','scan_state','preview_state','object_key','object_version','object_etag','state','cleanup_owner','cleanup_deadline','deletion_receipt_digest','updated_at'])
   IS NOT DISTINCT FROM(to_jsonb(new_row)-ARRAY['ref','file_name','media_type','digest','size_bytes','scan_state','preview_state','object_key','object_version','object_etag','state','cleanup_owner','cleanup_deadline','deletion_receipt_digest','updated_at'])
  AND EXISTS(SELECT 1 FROM control_plane.project_purge_context context
   JOIN control_plane.project_purge_receipts receipt ON receipt.project_id=context.project_id AND receipt.organization_id=(old_row).organization_id
   JOIN control_plane.project_purge_recovery_context scrub ON scrub.transaction_id=context.transaction_id AND scrub.backend_pid=context.backend_pid
    AND scrub.ledger_id=(old_row).id AND scrub.purpose='SCRUB'
   WHERE context.transaction_id=pg_current_xact_id_if_assigned() AND context.backend_pid=pg_backend_pid()
    AND context.project_id=(old_row).project_id AND receipt.state='OBJECTS_CLEARED'
    AND (new_row).deletion_receipt_digest=encode(public.digest(convert_to(jsonb_build_object(
     'kind','KUBERNETES_PROJECT_PURGE','ledger',(old_row).id,'project',(old_row).project_id,
     'objectsDigest',receipt.objects_digest,'objectKey',(old_row).object_key,
     'objectVersion',nullif((old_row).object_version,''),
     'objectETag',nullif((old_row).object_etag,''),
     'digest',(old_row).digest,'size',(old_row).size_bytes)::text,'UTF8'),'sha256'),'hex'));
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.kubernetes_project_prepared_scrub_valid(control_plane.prepared_content,control_plane.prepared_content) FROM PUBLIC,control_plane_runtime,artifact_retention_runtime;

-- +goose StatementBegin
CREATE FUNCTION control_plane.kubernetes_project_prepare_known_content(p_org uuid,p_project uuid,p_digest text)
RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE item control_plane.prepared_content%ROWTYPE;
BEGIN
 IF NOT EXISTS(SELECT 1 FROM control_plane.project_purge_context context
  JOIN control_plane.project_purge_receipts receipt ON receipt.project_id=context.project_id
  WHERE context.transaction_id=pg_current_xact_id_if_assigned() AND context.backend_pid=pg_backend_pid()
   AND context.project_id=p_project AND receipt.organization_id=p_org
   AND receipt.state='OBJECTS_CLEARED' AND receipt.objects_digest=p_digest)
  OR control_plane.project_purge_inventory_digest(p_org,p_project) IS DISTINCT FROM p_digest
  OR control_plane.kubernetes_project_unreconciled_count(p_org,p_project)>0 THEN
  RAISE EXCEPTION 'Kubernetes project purge proof is invalid' USING ERRCODE='42501'; END IF;
 PERFORM 1 FROM control_plane.prepared_content WHERE organization_id=p_org AND project_id=p_project ORDER BY id FOR UPDATE;
 FOR item IN SELECT prepared.* FROM control_plane.prepared_content prepared
  WHERE prepared.organization_id=p_org AND prepared.project_id=p_project
   AND control_plane.kubernetes_prepared_project_purge_eligible(prepared.id,p_org,p_project) ORDER BY prepared.id
 LOOP
  INSERT INTO control_plane.project_purge_recovery_context VALUES(pg_current_xact_id(),pg_backend_pid(),item.id,'SCRUB');
  UPDATE control_plane.prepared_content SET state='CLEANED',ref=NULL,file_name=NULL,media_type=NULL,
   digest=NULL,size_bytes=NULL,scan_state=NULL,preview_state=NULL,object_key=NULL,object_version=NULL,object_etag=NULL,
   cleanup_owner=NULL,cleanup_deadline=NULL,deletion_receipt_digest=encode(public.digest(convert_to(jsonb_build_object(
    'kind','KUBERNETES_PROJECT_PURGE','ledger',item.id,'project',p_project,'objectsDigest',p_digest,'objectKey',item.object_key,
    'objectVersion',nullif(item.object_version,''),
    'objectETag',nullif(item.object_etag,''),
    'digest',item.digest,'size',item.size_bytes)::text,'UTF8'),'sha256'),'hex'),updated_at=clock_timestamp()
   WHERE id=item.id;
  DELETE FROM control_plane.project_purge_recovery_context WHERE transaction_id=pg_current_xact_id()
   AND backend_pid=pg_backend_pid() AND ledger_id=item.id AND purpose='SCRUB';
 END LOOP;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.kubernetes_project_prepare_known_content(uuid,uuid,text) FROM PUBLIC,control_plane_runtime,artifact_retention_runtime;

-- Узкие predecessor markers сохраняют generic SYSTEM/PROJECT recovery и старые проверки purge.
-- +goose StatementBegin
DO $kubernetes_project_prepared_purge$
DECLARE definition text; marker text;
BEGIN
 SELECT pg_get_functiondef('control_plane.protect_prepared_content()'::regprocedure) INTO definition;
 marker:='  IF TG_OP=''DELETE'' THEN RAISE EXCEPTION ''prepared content intent cannot be deleted''; END IF;';
 IF(length(definition)-length(replace(definition,marker,'')))/length(marker)<>1 THEN
  RAISE EXCEPTION 'Kubernetes project prepared guard predecessor is unexpected'; END IF;
 EXECUTE replace(definition,marker,marker||E'
  IF TG_OP=''UPDATE'' AND current_user=''control_plane_owner'' THEN\n    IF control_plane.kubernetes_project_prepared_scrub_valid(OLD,NEW) THEN RETURN NEW; END IF;\n  END IF;');
 SELECT pg_get_functiondef('control_plane.project_purge_recovery_prepare_purge(uuid,uuid,text)'::regprocedure) INTO definition;
 marker:=' PERFORM 1 FROM control_plane.prepared_content content';
 IF(length(definition)-length(replace(definition,marker,'')))/length(marker)<>1 THEN
  RAISE EXCEPTION 'Kubernetes project prepared purge predecessor is unexpected'; END IF;
 EXECUTE replace(definition,marker,E' PERFORM control_plane.kubernetes_project_prepare_known_content(p_organization_id,p_project_id,p_objects_digest);
'||marker);
 -- Generic owner-abandonment не получает KUBE rows и не обнуляет их attempt history.
 FOREACH definition IN ARRAY ARRAY[
  pg_get_functiondef('control_plane.project_purge_recovery_snapshot(uuid,text,text)'::regprocedure),
  pg_get_functiondef('control_plane.project_purge_recovery_resolve(uuid,text,bigint,text,uuid,text,text)'::regprocedure)
 ] LOOP
  marker:='WHERE content.organization_id=p_organization_id AND content.origin_project_ref=p_project_ref';
  IF position(marker IN definition)=0 THEN RAISE EXCEPTION 'Kubernetes generic recovery predecessor is unexpected'; END IF;
  EXECUTE replace(definition,marker,marker||E'
      AND content.source_profile IN(''SYSTEM'',''PROJECT'')');
 END LOOP;
END $kubernetes_project_prepared_purge$;
-- +goose StatementEnd


-- Late ACK сохраняет immutable history и после Trash, но не выдаёт новый publication lease.
-- Restore требует явного repair прежней WAITING_OWNER generation, без native restart.
-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_kubernetes_capture_project_lifecycle() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE project_id uuid; lifecycle text;
BEGIN
 project_id:=nullif(NEW.source_snapshot::jsonb->>'projectId','')::uuid;
 IF project_id IS NULL THEN RETURN NULL; END IF;
 SELECT project.lifecycle INTO lifecycle FROM control_plane.projects project
  WHERE project.id=project_id AND project.organization_id=NEW.organization_id;
 IF lifecycle IS DISTINCT FROM 'ACTIVE' THEN
  UPDATE control_plane.kubernetes_output_captures SET state='WAITING_OWNER',reason_code='CAPTURE_ELIGIBILITY_CHANGED'
   WHERE id=NEW.id AND organization_id=NEW.organization_id AND state='READY';
 END IF;
 RETURN NULL;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_kubernetes_capture_project_lifecycle() FROM PUBLIC,control_plane_runtime,artifact_retention_runtime;
CREATE TRIGGER kubernetes_capture_project_lifecycle AFTER INSERT ON control_plane.kubernetes_output_captures
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_kubernetes_capture_project_lifecycle();

-- +goose StatementBegin
DO $kubernetes_capture_project_claim$
DECLARE definition text; marker text:='WHERE source.state IN(''READY'',''CLAIMED'') AND source.next_attempt_at<=clock_timestamp()';
BEGIN
 SELECT pg_get_functiondef('control_plane.kubernetes_capture_claim(text,integer,integer)'::regprocedure) INTO definition;
 IF(length(definition)-length(replace(definition,marker,'')))/length(marker)<>1 THEN
  RAISE EXCEPTION 'Kubernetes project capture claim predecessor is unexpected'; END IF;
 EXECUTE replace(definition,marker,marker||$project_claim$
   AND(coalesce(source.source_snapshot::jsonb->>'projectId','')='' OR EXISTS(
    SELECT 1 FROM control_plane.projects project
    WHERE project.id=(source.source_snapshot::jsonb->>'projectId')::uuid
     AND project.organization_id=source.organization_id AND project.lifecycle='ACTIVE'))$project_claim$);
END $kubernetes_capture_project_claim$;
-- +goose StatementEnd

-- Ручной purge использует ту же retention custody, но durable intent никогда не выдаёт
-- повторный DELETE уже допущенной версии. Источник намерения не является actor worker-а.
ALTER TABLE control_plane.artifact_heads
 ADD COLUMN manual_purge_actor_id uuid,
 ADD COLUMN manual_purge_operation text,
 ADD COLUMN manual_purge_key text,
 ADD CONSTRAINT artifact_manual_purge_receipt_shape CHECK(
  (manual_purge_actor_id IS NULL AND manual_purge_operation IS NULL AND manual_purge_key IS NULL)
  OR(manual_purge_actor_id IS NOT NULL AND manual_purge_operation IS NOT NULL AND manual_purge_operation='artifact.purge' AND manual_purge_key IS NOT NULL
     AND lifecycle_state IN('PURGE_PENDING','PURGED'))),
 ADD CONSTRAINT artifact_manual_purge_receipt_fk FOREIGN KEY(organization_id,manual_purge_actor_id,manual_purge_operation,manual_purge_key)
  REFERENCES control_plane.idempotency_receipts(organization_id,actor_id,operation,idempotency_key);

-- Счётчик читается после project FOR UPDATE отдельным свежим statement; manual
-- reservation держит тот же project FOR SHARE до durable intent COMMIT.
-- +goose StatementBegin
CREATE FUNCTION control_plane.artifact_manual_project_pending_count(p_org uuid,p_project uuid)
RETURNS bigint LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT count(*) FROM control_plane.artifact_heads head WHERE head.organization_id=p_org
  AND head.project_id=p_project AND head.lifecycle_state='PURGE_PENDING' AND head.manual_purge_actor_id IS NOT NULL;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.artifact_manual_project_pending_count(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.artifact_manual_project_pending_count(uuid,uuid) TO control_plane_runtime,artifact_retention_runtime;

-- После canonical project purge terminal receipt больше не удерживается удалённым head.
-- Обычная expiry очистит только безопасный terminal readback через прежние24часа.
-- +goose StatementBegin
CREATE FUNCTION control_plane.release_manual_artifact_project_receipt() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF OLD.manual_purge_actor_id IS NULL THEN RETURN NULL; END IF;
 IF NOT control_plane.project_purge_authorized() OR OLD.lifecycle_state<>'PURGED'
  OR OLD.retention_claim_owner IS NOT NULL OR OLD.retention_claim_expires_at IS NOT NULL THEN
  RAISE EXCEPTION 'manual artifact receipt release is not terminal'; END IF;
 UPDATE control_plane.idempotency_receipts SET expires_at=clock_timestamp()+interval '24 hours'
  WHERE organization_id=OLD.organization_id AND actor_id=OLD.manual_purge_actor_id
   AND operation=OLD.manual_purge_operation AND idempotency_key=OLD.manual_purge_key
   AND convert_from(response_payload,'UTF8')::jsonb->>'lifecycleState'='PURGED';
 IF NOT FOUND THEN RAISE EXCEPTION 'manual artifact terminal receipt is missing'; END IF;
 RETURN NULL;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.release_manual_artifact_project_receipt() FROM PUBLIC,control_plane_runtime,artifact_retention_runtime;
CREATE TRIGGER release_manual_artifact_project_receipt AFTER DELETE ON control_plane.artifact_heads
 FOR EACH ROW EXECUTE FUNCTION control_plane.release_manual_artifact_project_receipt();

-- Старый job не может присвоить manual intent прямым UPDATE и затем вызвать retrying Delete.
-- Новый consumer получает admission только из закрытой owner-функции ниже.
-- +goose StatementBegin
CREATE FUNCTION control_plane.protect_artifact_manual_custody() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF NEW.manual_purge_actor_id IS NOT NULL OR OLD.manual_purge_actor_id IS NOT NULL THEN
  IF (NEW.manual_purge_actor_id,NEW.manual_purge_operation,NEW.manual_purge_key)
      IS DISTINCT FROM(OLD.manual_purge_actor_id,OLD.manual_purge_operation,OLD.manual_purge_key)
     AND NOT(OLD.manual_purge_actor_id IS NULL AND OLD.lifecycle_state='DELETED'
       AND NEW.lifecycle_state='PURGE_PENDING' AND current_user='control_plane_owner') THEN
   RAISE EXCEPTION 'manual artifact purge origin is immutable'; END IF;
  IF current_user<>'control_plane_owner' AND
   (NEW.retention_claim_owner,NEW.retention_claim_generation,NEW.retention_claim_expires_at)
    IS DISTINCT FROM(OLD.retention_claim_owner,OLD.retention_claim_generation,OLD.retention_claim_expires_at)
    AND NOT(NEW.retention_claim_generation=OLD.retention_claim_generation
      AND NEW.retention_claim_owner IS NULL AND NEW.retention_claim_expires_at IS NULL) THEN
   RAISE EXCEPTION 'manual artifact purge requires typed custody'; END IF;
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER protect_artifact_manual_custody BEFORE UPDATE ON control_plane.artifact_heads
 FOR EACH ROW EXECUTE FUNCTION control_plane.protect_artifact_manual_custody();
REVOKE ALL ON FUNCTION control_plane.protect_artifact_manual_custody() FROM PUBLIC,control_plane_runtime,artifact_retention_runtime;

-- +goose StatementBegin
CREATE FUNCTION control_plane.artifact_manual_purge_issue(p_artifact uuid,p_version bigint,p_owner text,
 p_generation bigint,p_until timestamptz,p_actor uuid,p_key text)
RETURNS TABLE(artifact_id uuid) LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE head control_plane.artifact_heads%ROWTYPE; body jsonb;
BEGIN
 IF NOT pg_has_role(session_user,'control_plane_runtime','MEMBER') OR NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=session_user AND rolcanlogin) THEN RAISE EXCEPTION 'manual artifact issuer is inactive'; END IF;
 SELECT * INTO STRICT head FROM control_plane.artifact_heads WHERE id=p_artifact
  AND version=p_version AND lifecycle_state='DELETED' FOR UPDATE;
 IF p_owner IS NULL OR p_generation IS NULL OR p_until IS NULL OR length(p_owner) NOT BETWEEN 1 AND 128 OR p_generation<>head.retention_claim_generation+1
  OR p_until<=clock_timestamp() OR p_until>clock_timestamp()+interval '30 seconds'
  OR control_plane.artifact_has_retained_revisions(head.id) THEN RAISE EXCEPTION 'invalid manual purge custody'; END IF;
 SELECT convert_from(response_payload,'UTF8')::jsonb INTO STRICT body FROM control_plane.idempotency_receipts
  WHERE organization_id=head.organization_id AND actor_id=p_actor AND operation='artifact.purge'
   AND idempotency_key=p_key AND expires_at='infinity'::timestamptz FOR UPDATE;
 IF body->>'artifactId' IS DISTINCT FROM head.id::text OR body->>'artifactRef' IS DISTINCT FROM head.ref
  OR body->>'lifecycleState' IS DISTINCT FROM 'PURGE_PENDING' OR body->>'claimOwner' IS DISTINCT FROM p_owner
  OR (body->>'claimGeneration')::bigint IS DISTINCT FROM p_generation
  OR (body->>'claimUntil')::timestamptz IS DISTINCT FROM p_until THEN RAISE EXCEPTION 'manual purge intent mismatch'; END IF;
 RETURN QUERY UPDATE control_plane.artifact_heads SET lifecycle_state='PURGE_PENDING',version=version+1,
  retention_claim_owner=p_owner,retention_claim_generation=p_generation,retention_claim_expires_at=p_until,
  manual_purge_actor_id=p_actor,manual_purge_operation='artifact.purge',manual_purge_key=p_key
  WHERE id=p_artifact RETURNING id;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.artifact_manual_purge_issue(uuid,bigint,text,bigint,timestamptz,uuid,text) FROM PUBLIC,artifact_retention_runtime;
GRANT EXECUTE ON FUNCTION control_plane.artifact_manual_purge_issue(uuid,bigint,text,bigint,timestamptz,uuid,text) TO control_plane_runtime;

-- Root receipt хранит bounded объектный inventory и monotonic map допущенных revision ID.
-- Даже потерянный COMMIT ACK оставляет map: replay становится только Head, без нового DELETE.
-- +goose StatementBegin
CREATE FUNCTION control_plane.artifact_manual_purge_admit(p_org uuid,p_artifact uuid,p_actor uuid,p_key text,
 p_intent text,p_owner text,p_generation bigint,p_revision uuid)
RETURNS TABLE(admitted boolean,authorized_until timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE head control_plane.artifact_heads%ROWTYPE; body jsonb; object jsonb; matched integer;
BEGIN
 IF NOT(pg_has_role(session_user,'control_plane_runtime','MEMBER') OR pg_has_role(session_user,'artifact_retention_runtime','MEMBER'))
  OR NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=session_user AND rolcanlogin) THEN RAISE EXCEPTION 'manual artifact claimant is inactive'; END IF;
 SELECT * INTO STRICT head FROM control_plane.artifact_heads
  WHERE id=p_artifact AND organization_id=p_org AND lifecycle_state='PURGE_PENDING'
   AND manual_purge_actor_id=p_actor AND manual_purge_key=p_key
   AND retention_claim_owner=p_owner AND retention_claim_generation=p_generation
   AND retention_claim_expires_at>clock_timestamp() FOR UPDATE;
 IF control_plane.artifact_has_retained_revisions(head.id) THEN RAISE EXCEPTION 'artifact is retained'; END IF;
 SELECT convert_from(response_payload,'UTF8')::jsonb INTO STRICT body
  FROM control_plane.idempotency_receipts
  WHERE organization_id=p_org AND actor_id=p_actor AND operation='artifact.purge'
   AND idempotency_key=p_key AND intent_digest=p_intent AND expires_at='infinity'::timestamptz FOR UPDATE;
 IF body->>'artifactId' IS DISTINCT FROM p_artifact::text OR body->>'lifecycleState' IS DISTINCT FROM 'PURGE_PENDING'
   OR jsonb_typeof(body->'objects') IS DISTINCT FROM 'array' THEN RAISE EXCEPTION 'manual artifact purge receipt is invalid'; END IF;
 SELECT count(*) INTO matched FROM jsonb_array_elements(body->'objects') item
  JOIN control_plane.artifact_revision_content content ON content.revision_id=p_revision
  JOIN control_plane.artifact_revisions revision ON revision.id=content.revision_id AND revision.artifact_id=head.id
  WHERE item->>'revisionId'=p_revision::text AND item->>'objectKey'=content.object_key
   AND item->>'objectVersion'=content.object_version AND content.object_version<>'';
 IF matched<>1 THEN RAISE EXCEPTION 'manual artifact purge version is not pinned'; END IF;
 IF jsonb_typeof(coalesce(body->'deleteAttempts','{}'::jsonb)) IS DISTINCT FROM 'object' THEN RAISE EXCEPTION 'manual artifact attempt metadata is invalid'; END IF;
 admitted:=NOT(coalesce(body->'deleteAttempts','{}'::jsonb)?p_revision::text);
 authorized_until:=head.retention_claim_expires_at;
 IF admitted THEN
  object:=jsonb_build_object('owner',p_owner,'generation',p_generation,'authorizedUntil',authorized_until,
    'startedAt',clock_timestamp());
  body:=jsonb_set(body,ARRAY['deleteAttempts'],coalesce(body->'deleteAttempts','{}'::jsonb)||jsonb_build_object(p_revision::text,object));
  UPDATE control_plane.idempotency_receipts SET response_payload=convert_to(body::text,'UTF8')
   WHERE organization_id=p_org AND actor_id=p_actor AND operation='artifact.purge'
    AND idempotency_key=p_key AND intent_digest=p_intent;
 END IF;
 RETURN NEXT;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.artifact_manual_purge_admit(uuid,uuid,uuid,text,text,text,bigint,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.artifact_manual_purge_admit(uuid,uuid,uuid,text,text,text,bigint,uuid) TO control_plane_runtime;

-- Остальные ещё не допущенные версии получают новый bounded worker claim того же intent.
-- Уже допущенная версия получает только наблюдение, даже если прежний Delete завершился UNKNOWN.
-- +goose StatementBegin
CREATE FUNCTION control_plane.artifact_manual_retention_claim(p_owner text,p_limit integer,p_seconds integer)
RETURNS TABLE(artifact_id uuid,artifact_ref text,revision_id uuid,object_key text,object_version text,
 generation bigint,manual boolean,reconcile_only boolean,authorized_until timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE head control_plane.artifact_heads%ROWTYPE; selected record; intent text; admitted boolean;
BEGIN
 IF NOT pg_has_role(session_user,'artifact_retention_runtime','MEMBER') OR NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=session_user AND rolcanlogin)
  OR p_owner IS NULL OR p_limit IS NULL OR p_seconds IS NULL OR length(p_owner) NOT BETWEEN 1 AND 128 OR p_owner ~ '[[:space:][:cntrl:]]' OR p_limit NOT BETWEEN 0 AND 100
  OR p_seconds NOT BETWEEN 1 AND 600 THEN RAISE EXCEPTION 'invalid manual artifact retention claim'; END IF;
 FOR head IN SELECT candidate.* FROM control_plane.artifact_heads candidate
  WHERE candidate.manual_purge_actor_id IS NOT NULL AND candidate.lifecycle_state='PURGE_PENDING'
   AND(candidate.retention_claim_expires_at IS NULL OR candidate.retention_claim_expires_at<=clock_timestamp())
   AND candidate.retention_claim_generation<9223372036854775807
   AND NOT control_plane.artifact_has_retained_revisions(candidate.id)
  ORDER BY candidate.purge_after,candidate.id FOR UPDATE SKIP LOCKED LIMIT p_limit
 LOOP
  SELECT content.* INTO selected FROM control_plane.artifact_revisions revision
   JOIN control_plane.artifact_revision_content content ON content.revision_id=revision.id
   WHERE revision.artifact_id=head.id ORDER BY revision.revision,revision.id LIMIT 1;
  IF NOT FOUND THEN CONTINUE; END IF;
  SELECT receipt.intent_digest INTO STRICT intent FROM control_plane.idempotency_receipts receipt
   WHERE receipt.organization_id=head.organization_id AND receipt.actor_id=head.manual_purge_actor_id
    AND receipt.operation=head.manual_purge_operation AND receipt.idempotency_key=head.manual_purge_key
    AND receipt.expires_at='infinity'::timestamptz FOR UPDATE;
  UPDATE control_plane.artifact_heads SET retention_claim_owner=p_owner,
   retention_claim_generation=retention_claim_generation+1,
   retention_claim_expires_at=clock_timestamp()+make_interval(secs=>p_seconds),version=version+1
   WHERE id=head.id RETURNING * INTO head;
  SELECT admission.admitted,admission.authorized_until INTO admitted,authorized_until
   FROM control_plane.artifact_manual_purge_admit(head.organization_id,head.id,head.manual_purge_actor_id,
    head.manual_purge_key,intent,p_owner,head.retention_claim_generation,selected.revision_id) admission;
  UPDATE control_plane.idempotency_receipts SET response_payload=convert_to(
   jsonb_set(convert_from(response_payload,'UTF8')::jsonb,ARRAY['lastWorkerClaim'],
    jsonb_build_object('owner',p_owner,'generation',head.retention_claim_generation))::text,'UTF8')
   WHERE organization_id=head.organization_id AND actor_id=head.manual_purge_actor_id
    AND operation=head.manual_purge_operation AND idempotency_key=head.manual_purge_key;
  artifact_id:=head.id;artifact_ref:=head.ref;revision_id:=selected.revision_id;
  object_key:=selected.object_key;object_version:=selected.object_version;
  generation:=head.retention_claim_generation;manual:=true;reconcile_only:=NOT admitted;
  RETURN NEXT;
 END LOOP;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.artifact_manual_retention_claim(text,integer,integer) FROM PUBLIC,control_plane_runtime;
GRANT EXECUTE ON FUNCTION control_plane.artifact_manual_retention_claim(text,integer,integer) TO artifact_retention_runtime;

-- Завершение worker-ом сокращает original actor receipt, но audit actor остаётся SYSTEM.
-- +goose StatementBegin
CREATE FUNCTION control_plane.artifact_manual_retention_complete(p_artifact uuid,p_owner text,p_generation bigint)
RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE head control_plane.artifact_heads%ROWTYPE; body jsonb;
BEGIN
 IF NOT pg_has_role(session_user,'artifact_retention_runtime','MEMBER') OR NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=session_user AND rolcanlogin)
  OR p_owner IS NULL OR p_generation IS NULL OR p_generation<1 THEN RAISE EXCEPTION 'manual artifact terminal claim is invalid'; END IF;
 SELECT * INTO STRICT head FROM control_plane.artifact_heads WHERE id=p_artifact FOR UPDATE;
 IF head.manual_purge_actor_id IS NULL THEN RETURN; END IF;
 IF head.lifecycle_state<>'PURGED' OR head.retention_claim_generation<>p_generation
  OR head.retention_claim_owner IS NOT NULL OR head.retention_claim_expires_at IS NOT NULL
  OR EXISTS(SELECT 1 FROM control_plane.artifact_revisions revision
   JOIN control_plane.artifact_revision_content content ON content.revision_id=revision.id WHERE revision.artifact_id=head.id)
  THEN RAISE EXCEPTION 'manual artifact purge is not terminal'; END IF;
 SELECT convert_from(response_payload,'UTF8')::jsonb INTO STRICT body FROM control_plane.idempotency_receipts
  WHERE organization_id=head.organization_id AND actor_id=head.manual_purge_actor_id
   AND operation=head.manual_purge_operation AND idempotency_key=head.manual_purge_key FOR UPDATE;
 IF (body->'lastWorkerClaim'->>'owner') IS DISTINCT FROM p_owner
   OR (body->'lastWorkerClaim'->>'generation')::bigint IS DISTINCT FROM p_generation THEN
  RAISE EXCEPTION 'manual artifact terminal claim mismatch'; END IF;
 UPDATE control_plane.idempotency_receipts SET response_payload=convert_to(jsonb_build_object(
  'artifactRef',head.ref,'lifecycleState','PURGED','deleteAttemptCount',
  (SELECT count(*) FROM jsonb_object_keys(coalesce(body->'deleteAttempts','{}'::jsonb))))::text,'UTF8')
  WHERE organization_id=head.organization_id AND actor_id=head.manual_purge_actor_id
   AND operation=head.manual_purge_operation AND idempotency_key=head.manual_purge_key;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.artifact_manual_retention_complete(uuid,text,bigint) FROM PUBLIC,control_plane_runtime;
GRANT EXECUTE ON FUNCTION control_plane.artifact_manual_retention_complete(uuid,text,bigint) TO artifact_retention_runtime;

-- Terminal origin release использует прежний cleanup consumer, без нового loop или effect grant.
-- Порядок invocation -> все sources -> origin исключает source -> invocation inversion.
-- +goose StatementBegin
CREATE FUNCTION control_plane.kubernetes_file_origins_release(p_limit integer) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE candidate record; locked_sources bigint; total_sources bigint; released integer:=0;
BEGIN
 IF NOT pg_has_role(session_user,'artifact_retention_runtime','MEMBER') OR NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=session_user AND rolcanlogin)
  OR p_limit IS NULL OR p_limit NOT BETWEEN 1 AND 100 THEN RAISE EXCEPTION 'Kubernetes FILE origin release caller is invalid'; END IF;
 FOR candidate IN SELECT origin.invocation_id,origin.organization_id FROM control_plane.kubernetes_file_input_origins origin
  JOIN control_plane.integration_invocations invocation ON invocation.id=origin.invocation_id AND invocation.organization_id=origin.organization_id
  WHERE origin.artifact_revision_id IS NOT NULL AND invocation.state IN('SUCCEEDED','FAILED','REJECTED','CANCELLED','UNKNOWN_OUTCOME')
   AND control_plane.kubernetes_file_origin_release_eligible(origin.invocation_id,origin.organization_id)
  ORDER BY origin.invocation_id LIMIT p_limit*4
 LOOP
  EXIT WHEN released>=p_limit;
  PERFORM 1 FROM control_plane.integration_invocations invocation WHERE invocation.id=candidate.invocation_id
   AND invocation.organization_id=candidate.organization_id AND invocation.state IN('SUCCEEDED','FAILED','REJECTED','CANCELLED','UNKNOWN_OUTCOME') FOR UPDATE SKIP LOCKED;
  IF NOT FOUND THEN CONTINUE; END IF;
  -- UNIQUE(child_id,organization_id) и canonical plan bound ограничивают число source locks.
  PERFORM 1 FROM control_plane.kubernetes_file_input_sources source WHERE source.invocation_id=candidate.invocation_id
   AND source.organization_id=candidate.organization_id ORDER BY source.id FOR UPDATE SKIP LOCKED;
  GET DIAGNOSTICS locked_sources=ROW_COUNT;
  SELECT count(*) INTO total_sources FROM control_plane.kubernetes_file_input_sources source
   WHERE source.invocation_id=candidate.invocation_id AND source.organization_id=candidate.organization_id;
  IF locked_sources<>total_sources THEN CONTINUE; END IF;
  PERFORM 1 FROM control_plane.kubernetes_file_input_origins origin WHERE origin.invocation_id=candidate.invocation_id
   AND origin.organization_id=candidate.organization_id AND origin.artifact_revision_id IS NOT NULL FOR UPDATE SKIP LOCKED;
  IF NOT FOUND OR NOT control_plane.kubernetes_file_origin_release_eligible(candidate.invocation_id,candidate.organization_id) THEN CONTINUE; END IF;
  UPDATE control_plane.kubernetes_file_input_origins origin SET artifact_revision_id=NULL
   WHERE origin.invocation_id=candidate.invocation_id AND origin.organization_id=candidate.organization_id;
  released:=released+1;
 END LOOP;
 RETURN released;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.kubernetes_file_origins_release(integer) FROM PUBLIC,control_plane_runtime,artifact_retention_runtime;

-- Original FILE revision удерживается до отдельного owner terminal release.
-- Head lifecycle/retention и manual purge проверяют один и тот же authoritative rule до DELETE.
-- +goose StatementBegin
DO $file_origin_retention$
DECLARE definition text; marker text:='WHERE artifact.id=p_artifact_id AND (';
BEGIN
 SELECT pg_get_functiondef('control_plane.artifact_has_retained_revisions(uuid)'::regprocedure) INTO definition;
 IF(length(definition)-length(replace(definition,marker,'')))/length(marker)<>1 THEN
  RAISE EXCEPTION 'Kubernetes FILE artifact retention predecessor is unexpected'; END IF;
 EXECUTE replace(definition,marker,marker||E'\n            control_plane.kubernetes_file_origin_retained(artifact.id) OR');
END $file_origin_retention$;
-- +goose StatementEnd

-- FILE prepared intent переживает project graph как metadata-only ledger. Только уже
-- подтверждённая source deletion и released origin разрешают удалить его project/source FK.
-- +goose StatementBegin
CREATE FUNCTION control_plane.kubernetes_file_project_prepared_detach_valid(old_row control_plane.prepared_content,new_row control_plane.prepared_content)
RETURNS boolean LANGUAGE sql VOLATILE SET search_path=pg_catalog AS $$
 SELECT current_user='control_plane_owner' AND (old_row).source_profile='KUBERNETES_FILE_SOURCE'
  AND (old_row).project_id IS NOT NULL AND (old_row).state IN('ADOPTED','CLEANED')
  AND (old_row).deletion_receipt_digest IS NOT NULL AND (new_row).deletion_receipt_digest=(old_row).deletion_receipt_digest
  AND (new_row).state='CLEANED' AND (new_row).project_id IS NULL AND (new_row).adopted_file_source_id IS NULL
  AND (new_row).target_artifact_id IS NULL AND (new_row).plan_id IS NULL AND (new_row).plan_revision_id IS NULL
  AND (new_row).ref IS NULL AND (new_row).digest IS NULL AND (new_row).size_bytes IS NULL
  AND (new_row).object_key IS NULL AND (new_row).object_version IS NULL AND (new_row).object_etag IS NULL
  AND (new_row).cleanup_owner IS NULL AND (new_row).cleanup_deadline IS NULL
  AND(to_jsonb(old_row)-ARRAY['project_id','adopted_file_source_id','state','target_artifact_id','plan_id','plan_revision_id','updated_at'])
   IS NOT DISTINCT FROM(to_jsonb(new_row)-ARRAY['project_id','adopted_file_source_id','state','target_artifact_id','plan_id','plan_revision_id','updated_at'])
  AND EXISTS(SELECT 1 FROM control_plane.kubernetes_file_input_sources source
   JOIN control_plane.kubernetes_file_input_origins origin ON origin.invocation_id=source.invocation_id AND origin.organization_id=source.organization_id
   JOIN control_plane.integration_invocations invocation ON invocation.id=source.invocation_id AND invocation.organization_id=source.organization_id
   WHERE source.organization_id=(old_row).organization_id AND source.project_id=(old_row).project_id
    AND source.ref=(old_row).source_profile_ref AND(source.prepared_content_id IS NULL OR source.prepared_content_id=(old_row).id)
    AND((old_row).adopted_file_source_id IS NULL OR (old_row).adopted_file_source_id=source.id)
    AND source.cleanup_generation>0 AND source.payload_purged_at IS NOT NULL
    AND source.deletion_receipt_digest=(old_row).deletion_receipt_digest AND source.cleanup_owner IS NULL AND source.cleanup_deadline IS NULL
    AND(source.writer_completed_at IS NOT NULL OR source.writer_deadline<=clock_timestamp())
    AND origin.artifact_revision_id IS NULL AND invocation.state IN('SUCCEEDED','FAILED','REJECTED','CANCELLED','UNKNOWN_OUTCOME'))
  AND EXISTS(SELECT 1 FROM control_plane.project_purge_context context
   JOIN control_plane.project_purge_receipts receipt ON receipt.project_id=context.project_id AND receipt.organization_id=(old_row).organization_id
   JOIN control_plane.project_purge_recovery_context scrub ON scrub.transaction_id=context.transaction_id AND scrub.backend_pid=context.backend_pid
    AND scrub.ledger_id=(old_row).id AND scrub.purpose='SCRUB'
   WHERE context.transaction_id=pg_current_xact_id_if_assigned() AND context.backend_pid=pg_backend_pid()
    AND context.project_id=(old_row).project_id AND receipt.state='OBJECTS_CLEARED');
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.kubernetes_file_project_prepared_detach_valid(control_plane.prepared_content,control_plane.prepared_content) FROM PUBLIC,control_plane_runtime,artifact_retention_runtime;

-- Owner dispatcher не выполняет DELETE/PUT: он размыкает только уже очищенные typed FK.
-- +goose StatementBegin
CREATE FUNCTION control_plane.kubernetes_project_detach_file_prepared(p_org uuid,p_project uuid,p_digest text) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE item control_plane.prepared_content%ROWTYPE;
BEGIN
 IF NOT EXISTS(SELECT 1 FROM control_plane.project_purge_context context
  JOIN control_plane.project_purge_receipts receipt ON receipt.project_id=context.project_id AND receipt.organization_id=p_org
  JOIN control_plane.projects project ON project.id=context.project_id AND project.organization_id=p_org
  WHERE context.transaction_id=pg_current_xact_id_if_assigned() AND context.backend_pid=pg_backend_pid()
   AND context.project_id=p_project AND receipt.state='OBJECTS_CLEARED' AND receipt.objects_digest=p_digest AND project.lifecycle='PURGE_PENDING')
  OR control_plane.project_purge_inventory_digest(p_org,p_project) IS DISTINCT FROM p_digest
  OR control_plane.kubernetes_project_unreconciled_count(p_org,p_project)>0 THEN
  RAISE EXCEPTION 'Kubernetes FILE project detach proof is invalid' USING ERRCODE='42501'; END IF;
 FOR item IN SELECT content.* FROM control_plane.prepared_content content
  WHERE content.organization_id=p_org AND content.project_id=p_project AND content.source_profile='KUBERNETES_FILE_SOURCE'
  ORDER BY content.id FOR UPDATE
 LOOP
  INSERT INTO control_plane.project_purge_recovery_context VALUES(pg_current_xact_id(),pg_backend_pid(),item.id,'SCRUB');
  UPDATE control_plane.prepared_content SET state='CLEANED',project_id=NULL,adopted_file_source_id=NULL,
   target_artifact_id=NULL,plan_id=NULL,plan_revision_id=NULL,updated_at=clock_timestamp() WHERE id=item.id;
  DELETE FROM control_plane.project_purge_recovery_context WHERE transaction_id=pg_current_xact_id()
   AND backend_pid=pg_backend_pid() AND ledger_id=item.id AND purpose='SCRUB';
 END LOOP;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.kubernetes_project_detach_file_prepared(uuid,uuid,text) FROM PUBLIC,control_plane_runtime,artifact_retention_runtime;

-- Два прежних guard вызывают один exact typed predicate только под owner role.
-- +goose StatementBegin
DO $file_project_detach_guards$
DECLARE definition text; marker text;
BEGIN
 SELECT pg_get_functiondef('control_plane.guard_kubernetes_file_prepared()'::regprocedure) INTO definition;
 marker:=' IF TG_OP=''UPDATE'' AND OLD.deletion_receipt_digest IS NULL';
 IF(length(definition)-length(replace(definition,marker,'')))/length(marker)<>1 THEN RAISE EXCEPTION 'FILE prepared detach guard predecessor changed'; END IF;
 EXECUTE replace(definition,marker,E' IF TG_OP=''UPDATE'' AND current_user=''control_plane_owner'' THEN\n  IF control_plane.kubernetes_file_project_prepared_detach_valid(OLD,NEW) THEN RETURN NEW; END IF;\n END IF;\n'||marker);
 SELECT pg_get_functiondef('control_plane.protect_prepared_content()'::regprocedure) INTO definition;
 marker:='  IF TG_OP=''DELETE'' THEN RAISE EXCEPTION ''prepared content intent cannot be deleted''; END IF;';
 IF(length(definition)-length(replace(definition,marker,'')))/length(marker)<>1 THEN RAISE EXCEPTION 'FILE prepared detach owner predecessor changed'; END IF;
 EXECUTE replace(definition,marker,marker||E'\n  IF TG_OP=''UPDATE'' AND current_user=''control_plane_owner'' THEN\n    IF control_plane.kubernetes_file_project_prepared_detach_valid(OLD,NEW) THEN RETURN NEW; END IF;\n  END IF;');
 SELECT pg_get_functiondef('control_plane.project_purge_recovery_prepare_purge(uuid,uuid,text)'::regprocedure) INTO definition;
 marker:=' PERFORM control_plane.kubernetes_project_prepare_known_content(p_organization_id,p_project_id,p_objects_digest);';
 IF(length(definition)-length(replace(definition,marker,'')))/length(marker)<>1 THEN RAISE EXCEPTION 'FILE project detach dispatcher predecessor changed'; END IF;
 EXECUTE replace(definition,marker,marker||E'\n PERFORM control_plane.kubernetes_project_detach_file_prepared(p_organization_id,p_project_id,p_objects_digest);');
END $file_project_detach_guards$;
-- +goose StatementEnd

-- Точный actual catalog normal8bf: stage047; future graph variants не принимаются.
-- +goose StatementBegin
DO $purge_graph_047$
DECLARE definition text; parts text[]; total_nodes integer; nodes_digest text; total_edges integer; edges_digest text;
BEGIN
 IF(SELECT md5(prosrc) FROM pg_proc WHERE oid='control_plane.purge_project_database(uuid,uuid,text)'::regprocedure)<>'ed3fa56c744fec7d13d37d2c0d9f35c4' THEN
  RAISE EXCEPTION 'project purge stage047 predecessor function changed'; END IF;
 WITH RECURSIVE nodes(id) AS (
  SELECT 'control_plane.projects'::regclass::oid UNION
  SELECT bound.conrelid FROM nodes JOIN pg_constraint bound ON bound.confrelid=nodes.id
   WHERE bound.contype='f' AND bound.connamespace='control_plane'::regnamespace
 ) SELECT count(*),md5(string_agg(namespace.nspname||'.'||relation.relname,E'\n' ORDER BY namespace.nspname,relation.relname))
 INTO total_nodes,nodes_digest FROM nodes JOIN pg_class relation ON relation.oid=nodes.id JOIN pg_namespace namespace ON namespace.oid=relation.relnamespace;
 WITH RECURSIVE nodes(id) AS (
  SELECT 'control_plane.projects'::regclass::oid UNION
  SELECT bound.conrelid FROM nodes JOIN pg_constraint bound ON bound.confrelid=nodes.id
   WHERE bound.contype='f' AND bound.connamespace='control_plane'::regnamespace
 ) SELECT count(*),md5(string_agg(child_namespace.nspname||'.'||child.relname||'|'||bound.conname||'|'||parent_namespace.nspname||'.'||parent.relname||'|'||bound.confdeltype::text,
  E'\n' ORDER BY child_namespace.nspname,child.relname,bound.conname)) INTO total_edges,edges_digest
 FROM pg_constraint bound JOIN pg_class child ON child.oid=bound.conrelid JOIN pg_namespace child_namespace ON child_namespace.oid=child.relnamespace
 JOIN pg_class parent ON parent.oid=bound.confrelid JOIN pg_namespace parent_namespace ON parent_namespace.oid=parent.relnamespace
 WHERE bound.contype='f' AND bound.conrelid IN(SELECT id FROM nodes) AND bound.confrelid IN(SELECT id FROM nodes);
 IF ROW(total_nodes,nodes_digest,total_edges,edges_digest) IS DISTINCT FROM ROW(134,'ac4d979eccde3cb554e3818eb6e7445f',369,'c9a212a3f1bda48841abdc05eb573365') THEN
  RAISE EXCEPTION 'project purge stage047 exact graph changed'; END IF;
 SELECT pg_get_functiondef('control_plane.purge_project_database(uuid,uuid,text)'::regprocedure) INTO definition;
 definition:=replace(definition,'IF v_nodes <> 114 OR v_node_fingerprint <> ''7e9c4f53e840517ede4723bab15cbbd5'' THEN','IF v_nodes <> 134 OR v_node_fingerprint <> ''ac4d979eccde3cb554e3818eb6e7445f'' THEN');
 definition:=replace(definition,'IF v_edges <> 320 OR v_edge_fingerprint <> ''1cb01973881ffe170e65ca49f48a6289'' THEN','IF v_edges <> 369 OR v_edge_fingerprint <> ''c9a212a3f1bda48841abdc05eb573365'' THEN');
 parts:=string_to_array(definition,'ORDER BY target.relation_id');
 IF array_length(parts,1)<>3 THEN RAISE EXCEPTION 'Kubernetes purge deletion cursor changed'; END IF;
 definition:=parts[1]||'ORDER BY target.relation_id'||parts[2]||$order$ORDER BY CASE target.relation_id
          WHEN 'control_plane.kubernetes_file_delete_admissions'::regclass::oid THEN 0
          WHEN 'control_plane.kubernetes_file_input_frames'::regclass::oid THEN 1
          WHEN 'control_plane.kubernetes_capture_losses'::regclass::oid THEN 2
          WHEN 'control_plane.kubernetes_port_forward_frames'::regclass::oid THEN 3
          WHEN 'control_plane.kubernetes_port_forward_connections'::regclass::oid THEN 4
          WHEN 'control_plane.kubernetes_file_input_sources'::regclass::oid THEN 5
          WHEN 'control_plane.kubernetes_file_input_origins'::regclass::oid THEN 6
          ELSE 7 END,target.relation_id$order$||parts[3];
 EXECUTE definition;
END $purge_graph_047$;
-- +goose StatementEnd

-- Goose записывает собственную public metadata после тела миграции.
SET LOCAL search_path=public;
RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $forward_only$ BEGIN
 RAISE EXCEPTION 'managed Kubernetes schema is forward-only';
END $forward_only$;
-- +goose StatementEnd
