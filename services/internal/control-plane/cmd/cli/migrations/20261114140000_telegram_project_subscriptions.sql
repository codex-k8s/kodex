-- +goose Up
-- Подписка служебного бота принадлежит проекту; прежние Mattermost EACH gates не изменяются.
SET ROLE control_plane_owner;
SET LOCAL search_path=pg_catalog;
-- BEGIN GENERATED TELEGRAM NOTIFICATIONS PREDECESSOR
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
IF actual IS DISTINCT FROM $expected0${"authorityDigest":"9078371cb08839694c21e3d8750e8da1d309c6f45616662acbbc8d72eb0417eb","catalogDigest":"01cfac4486c0385d768376e5ebbeeef7d7e2e14b4c9b90f0d06f2345aca77317","functionCount":228,"functionsDigest":"8b2272337516e0974a537530e892cd6c675a194512b76e8513f9d45e0d6b8b8b","purgeEdgeCount":310,"purgeEdgeFingerprint":"2f32d3b42715f0a7c905ddc9f6957e71","purgeGraphDigest":"3bb0a85fbf658cf7cdb6af06a76f2e0c6328560bcdc63c459d5185cb174aba84","purgeNodeCount":112,"purgeNodeFingerprint":"97039cc15f63901977000dabe1167d5d","tableCount":168,"tableDefaultACL":["{control_plane_runtime=arwd/control_plane_owner}"],"tablesDigest":"0d91da72186e22f49e56b5828d41c21221d53f20b3a69fa0146e3214b4147cb3","triggerCount":180,"triggersDigest":"225cb8882f62762c44b73ac5a877a2d062e6644d2903bfcf5be5312191e0265e"}$expected0$::jsonb AND actual IS DISTINCT FROM $expected1${"authorityDigest":"dbbc0b0fa31f980c0d6f63654f3830a268f596f4e39790b86fca5e58ff0e8583","catalogDigest":"7efe0778ea06685ae2bec2eb8cf6e8db476aa28c9154c89cce89ded4c08d4888","functionCount":228,"functionsDigest":"8b2272337516e0974a537530e892cd6c675a194512b76e8513f9d45e0d6b8b8b","purgeEdgeCount":310,"purgeEdgeFingerprint":"2f32d3b42715f0a7c905ddc9f6957e71","purgeGraphDigest":"3bb0a85fbf658cf7cdb6af06a76f2e0c6328560bcdc63c459d5185cb174aba84","purgeNodeCount":112,"purgeNodeFingerprint":"97039cc15f63901977000dabe1167d5d","tableCount":168,"tableDefaultACL":["{control_plane_runtime=arwd/control_plane_owner,kodex_backup_reader=r/control_plane_owner}"],"tablesDigest":"394202dda9b558f18777d1dabdf9e0129e5ce551de8b2e86d235a0782cdcb8ed","triggerCount":180,"triggersDigest":"225cb8882f62762c44b73ac5a877a2d062e6644d2903bfcf5be5312191e0265e"}$expected1$::jsonb AND actual IS DISTINCT FROM $expected2${"authorityDigest":"7c9c625617acd100ce4d5fe8c29722854d1083bf30f65aef0043a490f3dec03c","catalogDigest":"decd5c91c392daffc1476b84ef6db4b4d6b57a4203311bf3feb5abe839654814","functionCount":228,"functionsDigest":"8b2272337516e0974a537530e892cd6c675a194512b76e8513f9d45e0d6b8b8b","purgeEdgeCount":310,"purgeEdgeFingerprint":"2f32d3b42715f0a7c905ddc9f6957e71","purgeGraphDigest":"3bb0a85fbf658cf7cdb6af06a76f2e0c6328560bcdc63c459d5185cb174aba84","purgeNodeCount":112,"purgeNodeFingerprint":"97039cc15f63901977000dabe1167d5d","tableCount":168,"tableDefaultACL":["{control_plane_runtime=arwd/control_plane_owner,kodex_backup_reader=r/control_plane_owner}"],"tablesDigest":"394202dda9b558f18777d1dabdf9e0129e5ce551de8b2e86d235a0782cdcb8ed","triggerCount":180,"triggersDigest":"225cb8882f62762c44b73ac5a877a2d062e6644d2903bfcf5be5312191e0265e"}$expected2$::jsonb THEN RAISE EXCEPTION 'Telegram notifications exact predecessor catalog changed';END IF;
END $closed_predecessor$;
-- +goose StatementEnd
-- END GENERATED TELEGRAM NOTIFICATIONS PREDECESSOR

-- +goose StatementBegin
DO $adapter_successor$
DECLARE item record; source text;
BEGIN
 SELECT conname,pg_get_constraintdef(oid) AS definition INTO STRICT item
 FROM pg_constraint WHERE conrelid='control_plane.integration_definitions'::regclass
 AND conname='integration_definitions_adapter_check' AND contype='c';
 source:=item.definition;
 IF (length(source)-length(replace(source,'''OBSERVABILITY''::text','')))/length('''OBSERVABILITY''::text')<>1
  OR position('TELEGRAM_INTERACTION' IN source)>0 THEN
  RAISE EXCEPTION 'telegram adapter predecessor changed';
 END IF;
 EXECUTE format('ALTER TABLE control_plane.integration_definitions DROP CONSTRAINT %I, ADD CONSTRAINT %I %s',
 item.conname,item.conname,replace(source,'''OBSERVABILITY''::text','''OBSERVABILITY''::text, ''TELEGRAM_INTERACTION''::text'));
END $adapter_successor$;
-- +goose StatementEnd

CREATE TABLE control_plane.telegram_subscriptions (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 ref text NOT NULL UNIQUE CHECK (ref ~ '^tgs_[A-Za-z0-9_-]{8,92}$'),
 organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
 project_id uuid NOT NULL UNIQUE REFERENCES control_plane.projects(id) ON DELETE CASCADE,
 connection_id uuid NOT NULL REFERENCES control_plane.integration_connections(id),
 connection_version bigint NOT NULL CHECK (connection_version>0),
 credential_revision_id uuid NOT NULL REFERENCES control_plane.integration_credential_revisions(id),
 definition_version text NOT NULL,
 definition_digest text NOT NULL CHECK (definition_digest ~ '^[a-f0-9]{64}$'),
 configuration jsonb NOT NULL CHECK (jsonb_typeof(configuration)='object' AND octet_length(configuration::text)<=4096),
 event_types text[] NOT NULL CHECK (cardinality(event_types) BETWEEN 1 AND 3 AND event_types <@ ARRAY['FINISHED','FAILED','HUMAN_GATE']::text[]),
 scope_digest text NOT NULL CHECK (scope_digest ~ '^[a-f0-9]{64}$'),
 grant_id uuid REFERENCES control_plane.integration_grants(id) DEFERRABLE INITIALLY IMMEDIATE,
 enabled boolean NOT NULL,
 version bigint NOT NULL DEFAULT 1 CHECK (version>0),
 approved_by uuid NOT NULL REFERENCES control_plane.subjects(id),
 expires_at timestamptz NOT NULL,
 max_effects integer NOT NULL CHECK (max_effects BETWEEN 1 AND 10000),
 used_effects integer NOT NULL DEFAULT 0 CHECK (used_effects BETWEEN 0 AND max_effects),
 rate_window_started_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 rate_window_effects integer NOT NULL DEFAULT 0 CHECK (rate_window_effects BETWEEN 0 AND 20),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK (expires_at>created_at)
);

ALTER TABLE control_plane.integration_grants
 ADD COLUMN telegram_project_id uuid REFERENCES control_plane.projects(id) ON DELETE CASCADE,
 DROP CONSTRAINT integration_grants_target_kind_check,
 ADD CONSTRAINT integration_grants_target_kind_check CHECK (target_kind IN ('AGENT','WORKFLOW','TELEGRAM_SUBSCRIPTION')),
 ADD CONSTRAINT integration_grants_telegram_kind_check CHECK (
   (target_kind='TELEGRAM_SUBSCRIPTION' AND capability_key='telegram.notifications' AND risk='WRITE'
     AND approval_policy='HUMAN_SCOPED' AND resource_kind='HTTPS_RESOURCE' AND cardinality(approval_scope_paths)=0 AND telegram_project_id IS NOT NULL)
   OR (target_kind<>'TELEGRAM_SUBSCRIPTION' AND capability_key NOT LIKE 'telegram.%' AND telegram_project_id IS NULL));

ALTER TABLE control_plane.interaction_deliveries ALTER CONSTRAINT interaction_deliveries_grant_id_fkey DEFERRABLE INITIALLY IMMEDIATE;
ALTER TABLE control_plane.interaction_message_receipts ALTER CONSTRAINT interaction_message_receipts_grant_id_fkey DEFERRABLE INITIALLY IMMEDIATE;
ALTER TABLE control_plane.integration_invocations ALTER CONSTRAINT integration_invocations_grant_id_fkey DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE control_plane.interaction_deliveries
 DROP CONSTRAINT interaction_deliveries_capability_key_check,
 ADD CONSTRAINT interaction_deliveries_capability_key_check CHECK (capability_key IN (
   'mattermost.notifications','mattermost.result_mirror','mattermost.gate_decisions','mattermost.acknowledgements','telegram.notifications')),
 ADD COLUMN telegram_subscription_id uuid REFERENCES control_plane.telegram_subscriptions(id) ON DELETE CASCADE,
 ADD COLUMN telegram_subscription_version bigint,
 ADD COLUMN telegram_scope_digest text,
 ADD COLUMN telegram_credential_revision_id uuid REFERENCES control_plane.integration_credential_revisions(id),
 ADD COLUMN telegram_input jsonb,
 ADD COLUMN telegram_input_digest text,
 ADD COLUMN telegram_source_key text,
 ADD COLUMN telegram_gate_id uuid REFERENCES control_plane.owner_gates(id) DEFERRABLE INITIALLY IMMEDIATE,
 ADD COLUMN telegram_effect_started_at timestamptz,
 ADD COLUMN telegram_expires_at timestamptz,
 ADD COLUMN telegram_receipt jsonb,
 ADD CONSTRAINT interaction_delivery_telegram_pin CHECK (
  (capability_key='telegram.notifications' AND telegram_subscription_id IS NOT NULL AND telegram_subscription_version>0
   AND telegram_scope_digest ~ '^[a-f0-9]{64}$' AND telegram_credential_revision_id IS NOT NULL
   AND jsonb_typeof(telegram_input)='object' AND octet_length(telegram_input::text)<=8192
   AND telegram_input_digest ~ '^[a-f0-9]{64}$' AND length(telegram_source_key) BETWEEN 1 AND 160
   AND telegram_expires_at IS NOT NULL AND approval_gate_id IS NULL AND acceptance_receipt_id IS NULL) IS TRUE
  OR (capability_key<>'telegram.notifications' AND telegram_subscription_id IS NULL AND telegram_subscription_version IS NULL
   AND telegram_scope_digest IS NULL AND telegram_credential_revision_id IS NULL AND telegram_input IS NULL
   AND telegram_input_digest IS NULL AND telegram_source_key IS NULL AND telegram_effect_started_at IS NULL
   AND telegram_gate_id IS NULL AND telegram_expires_at IS NULL AND telegram_receipt IS NULL));

DROP INDEX control_plane.interaction_deliveries_effect;
CREATE UNIQUE INDEX interaction_deliveries_effect ON control_plane.interaction_deliveries
 (connection_id,capability_key,root_run_id,COALESCE(gate_id,'00000000-0000-0000-0000-000000000000'::uuid))
 WHERE acceptance_receipt_id IS NULL AND capability_key<>'telegram.notifications';

CREATE UNIQUE INDEX interaction_deliveries_telegram_source
 ON control_plane.interaction_deliveries(telegram_subscription_id,telegram_source_key)
 WHERE telegram_subscription_id IS NOT NULL;
CREATE INDEX telegram_subscriptions_active ON control_plane.telegram_subscriptions(organization_id,project_id)
 WHERE enabled;
GRANT SELECT,INSERT,UPDATE ON control_plane.telegram_subscriptions TO control_plane_runtime;
REVOKE DELETE,TRUNCATE ON control_plane.telegram_subscriptions FROM control_plane_runtime;

-- +goose StatementBegin
CREATE FUNCTION control_plane.telegram_subscription_current(subscription_id uuid)
RETURNS boolean LANGUAGE sql VOLATILE SET search_path=pg_catalog AS $current$
 SELECT EXISTS (
  SELECT 1 FROM control_plane.telegram_subscriptions subscription
  JOIN control_plane.projects project ON project.id=subscription.project_id AND project.organization_id=subscription.organization_id
  JOIN control_plane.subjects actor ON actor.id=subscription.approved_by AND actor.active
  JOIN control_plane.integration_connections connection ON connection.id=subscription.connection_id
   AND connection.organization_id=subscription.organization_id AND connection.definition_key='telegram'
   AND connection.lifecycle_state='ACTIVE' AND connection.enabled AND connection.state='CONNECTED'
   AND connection.credential_policy='PROJECT_SHARED' AND control_plane.integration_connection_configuration_ready(connection.id)
   AND connection.version=subscription.connection_version AND connection.definition_version=subscription.definition_version
   AND connection.definition_digest=subscription.definition_digest AND connection.credential_revision_id=subscription.credential_revision_id
   AND connection.public_configuration=subscription.configuration
  JOIN control_plane.integration_credential_revisions credential ON credential.id=subscription.credential_revision_id
   AND credential.organization_id=subscription.organization_id AND credential.connection_id=connection.id
  JOIN control_plane.integration_grants binding ON binding.id=subscription.grant_id
   AND binding.telegram_project_id=project.id AND binding.organization_id=subscription.organization_id AND binding.connection_id=connection.id
   AND binding.target_kind='TELEGRAM_SUBSCRIPTION' AND binding.target_ref=subscription.ref AND binding.enabled
   AND binding.capability_key='telegram.notifications' AND binding.approval_policy='HUMAN_SCOPED'
   AND binding.resource_scope_digest=subscription.scope_digest
   AND binding.definition_version=subscription.definition_version AND binding.definition_digest=subscription.definition_digest
  WHERE subscription.id=subscription_id AND subscription.enabled AND project.lifecycle='ACTIVE'
   AND control_plane.catalog_resource_visible(project.organization_id,actor.id,'project.manage','PROJECT',
    project.id,project.id,project.created_by,'{}'::jsonb,transaction_timestamp())
   AND control_plane.catalog_resource_visible(connection.organization_id,actor.id,'integration.manage','INTEGRATION',
    connection.id,NULL,connection.created_by,'{}'::jsonb,transaction_timestamp())
 );
$current$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.telegram_subscription_current(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.telegram_subscription_current(uuid) TO control_plane_runtime;

-- +goose StatementBegin
CREATE FUNCTION control_plane.telegram_subscription_eligible(subscription_id uuid,run_id uuid,gate_id uuid,event_type text)
RETURNS boolean LANGUAGE sql VOLATILE SET search_path=pg_catalog AS $eligible$
 SELECT EXISTS (
  SELECT 1 FROM control_plane.telegram_subscriptions subscription
  JOIN control_plane.projects project ON project.id=subscription.project_id AND project.organization_id=subscription.organization_id
  JOIN control_plane.subjects actor ON actor.id=subscription.approved_by AND actor.active
  JOIN control_plane.integration_connections connection ON connection.id=subscription.connection_id
   AND connection.organization_id=subscription.organization_id AND connection.definition_key='telegram'
   AND connection.lifecycle_state='ACTIVE' AND connection.enabled AND connection.state='CONNECTED'
   AND connection.credential_policy='PROJECT_SHARED' AND control_plane.integration_connection_configuration_ready(connection.id)
   AND connection.version=subscription.connection_version AND connection.definition_version=subscription.definition_version
   AND connection.definition_digest=subscription.definition_digest AND connection.credential_revision_id=subscription.credential_revision_id
   AND connection.public_configuration=subscription.configuration
  JOIN control_plane.integration_credential_revisions credential ON credential.id=subscription.credential_revision_id
   AND credential.organization_id=subscription.organization_id AND credential.connection_id=connection.id
  JOIN control_plane.integration_grants binding ON binding.id=subscription.grant_id
   AND binding.telegram_project_id=project.id
   AND binding.organization_id=subscription.organization_id AND binding.connection_id=connection.id
   AND binding.target_kind='TELEGRAM_SUBSCRIPTION' AND binding.target_ref=subscription.ref AND binding.enabled
   AND binding.capability_key='telegram.notifications' AND binding.approval_policy='HUMAN_SCOPED'
   AND binding.resource_scope_digest=subscription.scope_digest
   AND binding.definition_version=subscription.definition_version AND binding.definition_digest=subscription.definition_digest
  JOIN control_plane.runs run ON run.id=run_id AND run.organization_id=subscription.organization_id
   AND run.project_id=project.id AND run.id=run.root_run_id
  LEFT JOIN control_plane.owner_gates gate ON gate.id=gate_id AND gate.organization_id=subscription.organization_id
   AND gate.project_id=project.id AND gate.root_run_id=run.id
  WHERE subscription.id=subscription_id AND subscription.enabled AND subscription.expires_at>clock_timestamp()
   AND control_plane.telegram_subscription_current(subscription.id)
   AND subscription.used_effects<subscription.max_effects AND event_type=ANY(subscription.event_types)
   AND project.lifecycle='ACTIVE'
   AND ((event_type='FINISHED' AND gate_id IS NULL AND run.state='SUCCEEDED')
    OR (event_type='FAILED' AND gate_id IS NULL AND run.state='FAILED')
    OR (event_type='HUMAN_GATE' AND gate.state='OPEN' AND run.state NOT IN ('SUCCEEDED','FAILED','CANCELLED','CANCELLING')))
   AND control_plane.catalog_resource_visible(project.organization_id,actor.id,'project.manage','PROJECT',
    project.id,project.id,project.created_by,'{}'::jsonb,transaction_timestamp())
   AND control_plane.catalog_resource_visible(connection.organization_id,actor.id,'integration.manage','INTEGRATION',
    connection.id,NULL,connection.created_by,'{}'::jsonb,transaction_timestamp())
   AND EXISTS (SELECT 1 FROM control_plane.catalog_access_targets target
    WHERE target.organization_id=run.organization_id AND target.kind='RUN' AND target.id=run.id
    AND control_plane.catalog_resource_visible(run.organization_id,actor.id,'run.view',target.kind,
     target.id,target.project_id,target.owner_id,target.related_ids,transaction_timestamp()))
 );
$eligible$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.telegram_subscription_eligible(uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.telegram_subscription_eligible(uuid,uuid,uuid,text) TO control_plane_runtime;

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_telegram_subscription() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $guard$
DECLARE subscription control_plane.telegram_subscriptions%ROWTYPE;
BEGIN
 IF TG_OP='DELETE' THEN
  IF TG_TABLE_NAME='telegram_subscriptions' AND control_plane.project_purge_authorized() AND EXISTS (
   SELECT 1 FROM control_plane.project_purge_targets target JOIN control_plane.project_purge_context context USING(transaction_id)
   WHERE target.relation_id=TG_RELID AND target.row_key=jsonb_build_array(OLD.id)
    AND context.project_id=OLD.project_id AND context.transaction_id=pg_current_xact_id_if_assigned()
    AND context.backend_pid=pg_backend_pid()) THEN RETURN OLD; END IF;
  RAISE EXCEPTION 'telegram subscription requires exact project purge authority';
 END IF;
 IF TG_TABLE_NAME='telegram_subscriptions' THEN
  IF TG_OP='UPDATE' AND (NEW.id,NEW.ref,NEW.organization_id,NEW.project_id,NEW.created_at)
   IS DISTINCT FROM (OLD.id,OLD.ref,OLD.organization_id,OLD.project_id,OLD.created_at) THEN
   RAISE EXCEPTION 'telegram subscription ownership is immutable';
  END IF;
  SELECT * INTO subscription FROM control_plane.telegram_subscriptions WHERE id=NEW.id;
 ELSE
  IF TG_OP='UPDATE' AND OLD.target_kind='TELEGRAM_SUBSCRIPTION'
   AND (NEW.organization_id,NEW.connection_id,NEW.telegram_project_id,NEW.capability_key,NEW.target_kind,NEW.target_ref)
    IS DISTINCT FROM (OLD.organization_id,OLD.connection_id,OLD.telegram_project_id,OLD.capability_key,OLD.target_kind,OLD.target_ref) THEN
   RAISE EXCEPTION 'telegram grant ownership is immutable';
  END IF;
  IF NEW.target_kind<>'TELEGRAM_SUBSCRIPTION' THEN RETURN NULL; END IF;
  SELECT * INTO subscription FROM control_plane.telegram_subscriptions
   WHERE organization_id=NEW.organization_id AND ref=NEW.target_ref;
  IF NOT FOUND THEN RAISE EXCEPTION 'telegram grant requires its exact subscription owner'; END IF;
 END IF;
 IF NOT FOUND THEN RETURN NULL; END IF;
 IF NOT EXISTS(SELECT 1 FROM control_plane.projects project JOIN control_plane.integration_connections connection
   ON connection.id=subscription.connection_id AND connection.organization_id=project.organization_id
  JOIN control_plane.integration_credential_revisions credential ON credential.id=subscription.credential_revision_id
   AND credential.organization_id=subscription.organization_id AND credential.connection_id=connection.id
  WHERE project.id=subscription.project_id AND project.organization_id=subscription.organization_id
   AND connection.definition_key='telegram' AND (NOT subscription.enabled OR connection.credential_policy='PROJECT_SHARED')) THEN
  RAISE EXCEPTION 'telegram subscription tenant and service credential pins are invalid';
 END IF;
 IF subscription.enabled AND NOT EXISTS(SELECT 1 FROM control_plane.integration_grants binding
  WHERE binding.id=subscription.grant_id AND binding.organization_id=subscription.organization_id
   AND binding.connection_id=subscription.connection_id AND binding.telegram_project_id=subscription.project_id AND binding.target_kind='TELEGRAM_SUBSCRIPTION'
   AND binding.target_ref=subscription.ref AND binding.capability_key='telegram.notifications' AND binding.enabled
   AND binding.resource_scope_digest=subscription.scope_digest AND binding.definition_version=subscription.definition_version
   AND binding.definition_digest=subscription.definition_digest) THEN
  RAISE EXCEPTION 'telegram subscription requires its exact bounded grant';
 END IF;
 RETURN NULL;
END $guard$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_telegram_subscription() FROM PUBLIC;
CREATE CONSTRAINT TRIGGER telegram_subscription_owner_guard AFTER INSERT OR UPDATE ON control_plane.telegram_subscriptions
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION control_plane.guard_telegram_subscription();
CREATE CONSTRAINT TRIGGER telegram_grant_owner_guard AFTER INSERT OR UPDATE ON control_plane.integration_grants
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION control_plane.guard_telegram_subscription();
CREATE TRIGGER telegram_subscription_delete_guard BEFORE DELETE ON control_plane.telegram_subscriptions
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_telegram_subscription();

-- +goose StatementBegin
CREATE FUNCTION control_plane.revoke_telegram_subscriptions() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $revoke$
DECLARE target_connection uuid; target_project uuid; closed_gate uuid;
BEGIN
 IF TG_TABLE_NAME='integration_connections' THEN
  IF (NEW.version,NEW.enabled,NEW.lifecycle_state,NEW.credential_policy,NEW.credential_revision_id,NEW.definition_version,NEW.definition_digest,NEW.public_configuration)
   IS NOT DISTINCT FROM (OLD.version,OLD.enabled,OLD.lifecycle_state,OLD.credential_policy,OLD.credential_revision_id,OLD.definition_version,OLD.definition_digest,OLD.public_configuration) THEN RETURN NEW; END IF;
  UPDATE control_plane.telegram_subscriptions SET enabled=false,version=version+1,updated_at=clock_timestamp()
   WHERE connection_id=NEW.id AND enabled;
  target_connection:=NEW.id;
 ELSIF TG_TABLE_NAME='projects' THEN
  IF NEW.lifecycle IS NOT DISTINCT FROM OLD.lifecycle OR NEW.lifecycle='ACTIVE' THEN RETURN NEW; END IF;
  UPDATE control_plane.telegram_subscriptions SET enabled=false,version=version+1,updated_at=clock_timestamp()
   WHERE project_id=NEW.id AND enabled;
  target_project:=NEW.id;
 ELSIF TG_TABLE_NAME='owner_gates' THEN
  IF NEW.state='OPEN' OR NEW.state IS NOT DISTINCT FROM OLD.state THEN RETURN NEW; END IF;
  closed_gate:=NEW.id;
 END IF;
 UPDATE control_plane.integration_grants binding SET enabled=false,version=binding.version+1,updated_at=clock_timestamp()
 FROM control_plane.telegram_subscriptions subscription
 WHERE binding.id=subscription.grant_id AND NOT subscription.enabled AND binding.enabled
  AND (subscription.connection_id=target_connection OR subscription.project_id=target_project);
 UPDATE control_plane.interaction_deliveries delivery
 SET state=CASE WHEN delivery.telegram_effect_started_at IS NULL THEN 'CANCELLED' ELSE 'UNKNOWN_OUTCOME' END,
  safe_error_code=CASE WHEN delivery.telegram_effect_started_at IS NULL THEN 'TELEGRAM_SCOPE_REVOKED' ELSE 'TELEGRAM_OUTCOME_UNKNOWN' END,
  lease_ref=NULL,fence_digest=NULL,workload_instance=NULL,lease_expires_at=NULL,
  completed_at=clock_timestamp(),updated_at=clock_timestamp(),version=delivery.version+1
 FROM control_plane.telegram_subscriptions subscription
 WHERE delivery.telegram_subscription_id=subscription.id AND delivery.state IN ('DUE','FAILED','CLAIMED')
  AND ((NOT subscription.enabled AND (subscription.connection_id=target_connection OR subscription.project_id=target_project))
   OR delivery.telegram_gate_id=closed_gate);
 RETURN NEW;
END $revoke$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.revoke_telegram_subscriptions() FROM PUBLIC;
CREATE TRIGGER telegram_connection_revoke AFTER UPDATE ON control_plane.integration_connections
 FOR EACH ROW EXECUTE FUNCTION control_plane.revoke_telegram_subscriptions();
CREATE TRIGGER telegram_project_revoke AFTER UPDATE ON control_plane.projects
 FOR EACH ROW EXECUTE FUNCTION control_plane.revoke_telegram_subscriptions();
CREATE TRIGGER telegram_gate_revoke AFTER UPDATE ON control_plane.owner_gates
 FOR EACH ROW EXECUTE FUNCTION control_plane.revoke_telegram_subscriptions();

-- Каждый delivery сохраняет owner-assigned источник и снимок даже после отзыва либо замены подписки.
-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_telegram_delivery() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $delivery$
BEGIN
 IF TG_OP='UPDATE' AND (OLD.capability_key='telegram.notifications') IS DISTINCT FROM (NEW.capability_key='telegram.notifications') THEN
  RAISE EXCEPTION 'telegram delivery kind is immutable';
 END IF;
 IF NEW.capability_key<>'telegram.notifications' THEN RETURN NEW; END IF;
 IF TG_OP='UPDATE' THEN
  IF (NEW.id,NEW.ref,NEW.organization_id,NEW.project_id,NEW.connection_id,NEW.grant_id,NEW.root_run_id,
      NEW.capability_key,NEW.message_key,NEW.template_data,NEW.connection_version,NEW.definition_version,NEW.definition_digest,
      NEW.execution_max_attempts,NEW.telegram_subscription_id,NEW.telegram_subscription_version,NEW.telegram_scope_digest,
      NEW.telegram_credential_revision_id,NEW.telegram_input,NEW.telegram_input_digest,NEW.telegram_source_key,
      NEW.telegram_gate_id,NEW.telegram_expires_at,NEW.gate_id,NEW.approval_gate_id,NEW.acceptance_receipt_id,NEW.created_at)
   IS DISTINCT FROM
     (OLD.id,OLD.ref,OLD.organization_id,OLD.project_id,OLD.connection_id,OLD.grant_id,OLD.root_run_id,
      OLD.capability_key,OLD.message_key,OLD.template_data,OLD.connection_version,OLD.definition_version,OLD.definition_digest,
      OLD.execution_max_attempts,OLD.telegram_subscription_id,OLD.telegram_subscription_version,OLD.telegram_scope_digest,
      OLD.telegram_credential_revision_id,OLD.telegram_input,OLD.telegram_input_digest,OLD.telegram_source_key,
      OLD.telegram_gate_id,OLD.telegram_expires_at,OLD.gate_id,OLD.approval_gate_id,OLD.acceptance_receipt_id,OLD.created_at) THEN
   RAISE EXCEPTION 'telegram delivery source and input pins are immutable';
  END IF;
  IF OLD.state IN ('SUCCEEDED','CANCELLED','UNKNOWN_OUTCOME') AND NEW.state IS DISTINCT FROM OLD.state
   OR OLD.state='FAILED' AND OLD.completed_at IS NOT NULL AND NEW.state IS DISTINCT FROM OLD.state THEN
   RAISE EXCEPTION 'terminal telegram delivery cannot be retried';
  END IF;
  IF OLD.telegram_receipt IS NOT NULL AND NEW.telegram_receipt IS DISTINCT FROM OLD.telegram_receipt THEN
   RAISE EXCEPTION 'telegram delivery receipt is immutable';
  END IF;
  IF OLD.telegram_effect_started_at IS NOT NULL AND NEW.telegram_effect_started_at IS DISTINCT FROM OLD.telegram_effect_started_at
   AND NOT (NEW.telegram_effect_started_at IS NULL AND OLD.state='CLAIMED' AND NEW.state='FAILED'
    AND NEW.safe_error_code IN ('TELEGRAM_RATE_LIMITED','TELEGRAM_PREFLIGHT_UNAVAILABLE')
    AND OLD.attempt<OLD.execution_max_attempts AND NEW.completed_at IS NULL AND NEW.telegram_receipt IS NULL) THEN
   RAISE EXCEPTION 'telegram possible-effect marker cannot be reset';
  END IF;
  RETURN NEW;
 END IF;
 IF NOT EXISTS (
  SELECT 1 FROM control_plane.telegram_subscriptions subscription
  JOIN control_plane.projects project ON project.id=subscription.project_id AND project.organization_id=subscription.organization_id
  JOIN control_plane.runs run ON run.id=NEW.root_run_id AND run.root_run_id=run.id
   AND run.organization_id=subscription.organization_id AND run.project_id=project.id
  JOIN control_plane.integration_credential_revisions credential ON credential.id=subscription.credential_revision_id
  LEFT JOIN control_plane.owner_gates gate ON gate.id=NEW.telegram_gate_id AND gate.organization_id=subscription.organization_id
   AND gate.project_id=project.id AND gate.root_run_id=run.id
  WHERE subscription.id=NEW.telegram_subscription_id AND subscription.organization_id=NEW.organization_id
   AND subscription.project_id=NEW.project_id AND subscription.connection_id=NEW.connection_id AND subscription.grant_id=NEW.grant_id
   AND subscription.version=NEW.telegram_subscription_version AND subscription.scope_digest=NEW.telegram_scope_digest
   AND subscription.connection_version=NEW.connection_version AND subscription.definition_version=NEW.definition_version
   AND subscription.definition_digest=NEW.definition_digest AND subscription.credential_revision_id=NEW.telegram_credential_revision_id
   AND NEW.gate_id IS NULL AND NEW.approval_gate_id IS NULL AND NEW.acceptance_receipt_id IS NULL AND NEW.template_data='{}'::jsonb
   AND NEW.telegram_input->>'subscription_ref'=subscription.ref
   AND (NEW.telegram_input->>'subscription_version')::bigint=subscription.version
   AND NEW.telegram_input->>'scope_digest'=subscription.scope_digest AND NEW.telegram_input->>'project_ref'=project.ref
   AND NEW.telegram_input->>'run_ref'=run.ref AND (NEW.telegram_input->>'source_run_version')::bigint=run.version
   AND NEW.telegram_input->>'credential_revision_ref'=credential.ref
   AND (NEW.telegram_input->>'connection_version')::bigint=subscription.connection_version
   AND NEW.telegram_input->>'definition_digest'=subscription.definition_digest
   AND NEW.telegram_input->'configuration'=subscription.configuration
   AND (NEW.telegram_input->>'expires_at')::timestamptz=NEW.telegram_expires_at AND NEW.telegram_expires_at<=subscription.expires_at
   AND NEW.telegram_input->>'event_type'=ANY(subscription.event_types)
   AND NEW.message_key=NEW.telegram_input->>'template_key'
   AND ((NEW.telegram_input->>'event_type'='FINISHED' AND NEW.message_key='TELEGRAM_FINISHED_V1' AND run.state='SUCCEEDED'
     AND NEW.telegram_gate_id IS NULL AND NEW.telegram_input->>'gate_ref'='' AND NEW.telegram_input->>'gate_version'='0'
     AND NEW.telegram_source_key='run:'||run.ref||':FINISHED')
    OR (NEW.telegram_input->>'event_type'='FAILED' AND NEW.message_key='TELEGRAM_FAILED_V1' AND run.state='FAILED'
     AND NEW.telegram_gate_id IS NULL AND NEW.telegram_input->>'gate_ref'='' AND NEW.telegram_input->>'gate_version'='0'
     AND NEW.telegram_source_key='run:'||run.ref||':FAILED')
    OR (NEW.telegram_input->>'event_type'='HUMAN_GATE' AND NEW.message_key='TELEGRAM_HUMAN_GATE_V1' AND gate.state='OPEN'
     AND NEW.telegram_input->>'gate_ref'=gate.ref AND (NEW.telegram_input->>'gate_version')::bigint=gate.version
     AND NEW.telegram_source_key='gate:'||gate.ref||':'||gate.version::text))
 ) THEN RAISE EXCEPTION 'telegram delivery requires exact subscription and source ownership'; END IF;
 RETURN NEW;
END $delivery$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_telegram_delivery() FROM PUBLIC;
CREATE TRIGGER telegram_delivery_pin_guard BEFORE INSERT OR UPDATE ON control_plane.interaction_deliveries
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_telegram_delivery();

-- Граф purge расширяется только фактически захваченными двумя owner-узлами и десятью FK-рёбрами.
-- +goose StatementBegin
DO $telegram_purge$
DECLARE
 target oid:='control_plane.purge_project_database(uuid,uuid,text)'::regprocedure;
 definition text;source text;replacement text;nodes oid[];actual_count integer;actual_digest text;
 old_nodes text:='IF v_nodes <> 112 OR v_node_fingerprint <> ''97039cc15f63901977000dabe1167d5d'' THEN';
 old_edges text:='IF v_edges <> 310 OR v_edge_fingerprint <> ''2f32d3b42715f0a7c905ddc9f6957e71'' THEN';
 new_nodes text:='IF v_nodes <> 114 OR v_node_fingerprint <> ''7e9c4f53e840517ede4723bab15cbbd5'' THEN';
 new_edges text:='IF v_edges <> 320 OR v_edge_fingerprint <> ''1cb01973881ffe170e65ca49f48a6289'' THEN';
BEGIN
 SELECT pg_get_functiondef(p.oid),p.prosrc INTO STRICT definition,source FROM pg_proc p
 WHERE p.oid=target AND p.proowner='control_plane_owner'::regrole AND p.prosecdef
  AND p.proconfig=ARRAY['search_path=pg_catalog'] AND p.provolatile='v'
  AND has_function_privilege('control_plane_runtime',p.oid,'EXECUTE');
 IF regexp_count(source,'IF v_nodes <>')<>1 OR regexp_count(source,'IF v_edges <>')<>1
  OR md5(replace(replace(source,old_nodes,
   'IF v_nodes <> 105 OR v_node_fingerprint <> ''f51a0de25bd942a250997775617eeaee'' THEN'),old_edges,
   'IF v_edges <> 284 OR v_edge_fingerprint <> ''6bf6ad76d8c7ae66e07f75ecf719fdd6'' THEN'))<>'b886c24d4e2f5c4a0ec08123d99462b0'
 THEN RAISE EXCEPTION 'telegram purge predecessor changed'; END IF;
 WITH RECURSIVE graph(id) AS (SELECT 'control_plane.projects'::regclass::oid UNION
  SELECT c.conrelid FROM pg_constraint c JOIN graph n ON c.confrelid=n.id
  WHERE c.contype='f' AND c.connamespace='control_plane'::regnamespace)
 SELECT array_agg(id) INTO nodes FROM graph;
 SELECT count(*),md5(string_agg(n.nspname||'.'||r.relname,E'\n' ORDER BY n.nspname,r.relname)) INTO actual_count,actual_digest
 FROM pg_class r JOIN pg_namespace n ON n.oid=r.relnamespace WHERE r.oid=ANY(nodes);
 IF actual_count<>114 OR actual_digest<>'7e9c4f53e840517ede4723bab15cbbd5' THEN RAISE EXCEPTION 'telegram purge successor nodes changed';END IF;
 SELECT count(*),md5(string_agg(cn.nspname||'.'||cc.relname||'|'||c.conname||'|'||pn.nspname||'.'||pc.relname||'|'||c.confdeltype::text,
  E'\n' ORDER BY cn.nspname,cc.relname,c.conname)) INTO actual_count,actual_digest
 FROM pg_constraint c JOIN pg_class cc ON cc.oid=c.conrelid JOIN pg_namespace cn ON cn.oid=cc.relnamespace
 JOIN pg_class pc ON pc.oid=c.confrelid JOIN pg_namespace pn ON pn.oid=pc.relnamespace
 WHERE c.contype='f' AND c.conrelid=ANY(nodes) AND c.confrelid=ANY(nodes);
 IF actual_count<>320 OR actual_digest<>'1cb01973881ffe170e65ca49f48a6289' THEN RAISE EXCEPTION 'telegram purge successor edges changed';END IF;
 replacement:=replace(replace(definition,old_nodes,new_nodes),old_edges,new_edges);
 IF replacement=definition OR position(old_nodes IN replacement)>0 OR position(old_edges IN replacement)>0 THEN
  RAISE EXCEPTION 'telegram purge exact guard replacement failed';END IF;
 EXECUTE replacement;
 IF NOT EXISTS(SELECT 1 FROM pg_proc p WHERE p.oid=target
  AND p.prosrc=replace(replace(source,old_nodes,new_nodes),old_edges,new_edges)) THEN
  RAISE EXCEPTION 'telegram purge readback failed';END IF;
END $telegram_purge$;
-- +goose StatementEnd
SET LOCAL search_path=public;
RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $forward_only$ BEGIN
 RAISE EXCEPTION 'telegram subscription schema is forward-only';
END $forward_only$;
-- +goose StatementEnd
