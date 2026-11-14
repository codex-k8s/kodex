-- +goose Up
-- Расширение закрытого owner guard после GitHub scope; прежние данные и pins не переписываются.
SET ROLE control_plane_owner;
SET LOCAL search_path=pg_catalog;
-- BEGIN GENERATED SENTRY OBSERVABILITY PREDECESSOR
-- +goose StatementBegin
DO $closed_predecessor$
DECLARE actual jsonb;
BEGIN
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
IF actual IS DISTINCT FROM $normal${"authorityDigest":"9078371cb08839694c21e3d8750e8da1d309c6f45616662acbbc8d72eb0417eb","catalogDigest":"51193c2c3ee32376355a0b127374a7e7ecb2c6be1b0651f3504a99b9f0e04663","functionCount":228,"functionsDigest":"240a5c33ba9c97f398ae6d92e89d60d708913e98e264c180bb921a79ede1fc95","purgeEdgeCount":310,"purgeEdgeFingerprint":"2f32d3b42715f0a7c905ddc9f6957e71","purgeGraphDigest":"3bb0a85fbf658cf7cdb6af06a76f2e0c6328560bcdc63c459d5185cb174aba84","purgeNodeCount":112,"purgeNodeFingerprint":"97039cc15f63901977000dabe1167d5d","tableCount":168,"tableDefaultACL":["{control_plane_runtime=arwd/control_plane_owner}"],"tablesDigest":"a7490ee336efc72adea42a2d1959f6188c0ebb64abef6fcebdd3934d6e0704d1","triggerCount":180,"triggersDigest":"225cb8882f62762c44b73ac5a877a2d062e6644d2903bfcf5be5312191e0265e"}$normal$::jsonb AND actual IS DISTINCT FROM $backup${"authorityDigest":"7c9c625617acd100ce4d5fe8c29722854d1083bf30f65aef0043a490f3dec03c","catalogDigest":"565f3d9e08fb43227c49c471aac195c5fcd83cbdc6fc5bb8091a0c8384009e33","functionCount":228,"functionsDigest":"240a5c33ba9c97f398ae6d92e89d60d708913e98e264c180bb921a79ede1fc95","purgeEdgeCount":310,"purgeEdgeFingerprint":"2f32d3b42715f0a7c905ddc9f6957e71","purgeGraphDigest":"3bb0a85fbf658cf7cdb6af06a76f2e0c6328560bcdc63c459d5185cb174aba84","purgeNodeCount":112,"purgeNodeFingerprint":"97039cc15f63901977000dabe1167d5d","tableCount":168,"tableDefaultACL":["{control_plane_runtime=arwd/control_plane_owner,kodex_backup_reader=r/control_plane_owner}"],"tablesDigest":"5068c32013290fc741c0a211c2775ece625889ba77c9efd2bb71497144d1e650","triggerCount":180,"triggersDigest":"225cb8882f62762c44b73ac5a877a2d062e6644d2903bfcf5be5312191e0265e"}$backup$::jsonb THEN RAISE EXCEPTION 'Sentry observability exact predecessor catalog changed';END IF;
END $closed_predecessor$;
-- +goose StatementEnd
-- END GENERATED SENTRY OBSERVABILITY PREDECESSOR
SET LOCAL lock_timeout='8s';
SET LOCAL statement_timeout='30s';
-- +goose StatementBegin
DO $sentry_profiles$
DECLARE
 item record;
 source text;
 marker text;
 replacement text;
 target regclass;
 column_name text;
 matches integer;
BEGIN
 FOR target,column_name,marker,replacement IN VALUES
  ('control_plane.integration_definitions'::regclass,'adapter',
   '''SSH_COMPOSE''::text', '''SSH_COMPOSE''::text, ''SENTRY''::text, ''OBSERVABILITY''::text'),
  ('control_plane.integration_personal_invocation_credentials'::regclass,'resource_permission_profile',
   '''GITLAB_SCOPED_RESOURCE_V2''::text',
   '''GITLAB_SCOPED_RESOURCE_V2''::text, ''SENTRY_PROJECT_OBJECT_READ_V1''::text, ''SENTRY_ISSUE_STATUS_WRITE_V1''::text, ''SENTRY_PROJECT_MUTATION_V1''::text, ''SENTRY_ORGANIZATION_OBJECT_READ_V1''::text, ''SENTRY_ORGANIZATION_MUTATION_V1''::text, ''OBSERVABILITY_TYPED_RESOURCE_READ_V1''::text, ''GRAFANA_TYPED_WRITE_V1''::text, ''LOKI_RULER_MUTATION_V1''::text'),
  ('control_plane.integration_personal_credentials'::regclass,'observed_permission_profiles',
   '''GITLAB_PROJECT_WRITE_SCOPE_V1''::text',
   '''GITLAB_PROJECT_WRITE_SCOPE_V1''::text, ''SENTRY_TOKEN_SCOPES_V1''::text, ''GRAFANA_CURRENT_PERMISSIONS_V1''::text')
 LOOP
  matches:=0;
  FOR item IN SELECT constraint_row.conname,pg_get_constraintdef(constraint_row.oid) AS definition
   FROM pg_constraint constraint_row
   JOIN pg_attribute attribute ON attribute.attrelid=constraint_row.conrelid AND attribute.attname=column_name
   WHERE constraint_row.conrelid=target AND constraint_row.contype='c'
    AND constraint_row.conkey=ARRAY[attribute.attnum]::smallint[]
  LOOP
   matches:=matches+1;
   source:=item.definition;
   IF (length(source)-length(replace(source,marker,'')))/length(marker)<>1
    OR position('SENTRY_' IN source)>0 THEN RAISE EXCEPTION 'Sentry credential constraint predecessor changed'; END IF;
   EXECUTE format('ALTER TABLE %s DROP CONSTRAINT %I',target,item.conname);
   EXECUTE format('ALTER TABLE %s ADD CONSTRAINT %I %s',target,item.conname,replace(source,marker,replacement));
  END LOOP;
  IF matches<>1 THEN RAISE EXCEPTION 'Sentry credential constraint is missing or ambiguous'; END IF;
 END LOOP;
END $sentry_profiles$;
-- +goose StatementEnd
-- +goose StatementBegin
DO $sentry_guard$
DECLARE
 source text;
 marker text := $before$(NEW.resource_permission_profile='GITLAB_SCOPED_RESOURCE_V2'$before$;
 replacement text := $after$(connection.definition_key IN ('sentry','sentry-self-hosted')
             AND invocation.resource_kind='HTTPS_RESOURCE'
             AND invocation.operation=invocation.capability_key
             AND binding.verified_capabilities @> ARRAY['sentry.project.read']::text[]
             AND NOT binding.denied_capabilities && ARRAY['sentry.project.read']::text[]
             AND (
               (NEW.resource_permission_profile='SENTRY_PROJECT_OBJECT_READ_V1'
                AND invocation.risk='READ' AND invocation.approval_policy='NONE'
                AND invocation.capability_key IN ('sentry.project.read','sentry.issue.list','sentry.issue.read','sentry.event.read',
                  'sentry.issue.event.list','sentry.issue.comment.list','sentry.release.list','sentry.release.read','sentry.alert.list','sentry.alert.read'))
               OR (NEW.resource_permission_profile='SENTRY_ISSUE_STATUS_WRITE_V1'
                AND invocation.capability_key='sentry.issue.status.update'
                AND invocation.risk='WRITE' AND invocation.approval_policy IN ('HUMAN_EACH_EFFECT','HUMAN_SCOPED')
                AND binding.observed_permission_profiles @> ARRAY['SENTRY_TOKEN_SCOPES_V1']::text[])
               OR (NEW.resource_permission_profile='SENTRY_PROJECT_MUTATION_V1'
                AND invocation.capability_key IN ('sentry.issue.comment.create','sentry.issue.assignment.update')
                AND invocation.risk='WRITE' AND invocation.approval_policy IN ('HUMAN_EACH_EFFECT','HUMAN_SCOPED')
                AND binding.observed_permission_profiles @> ARRAY['SENTRY_TOKEN_SCOPES_V1']::text[])
             ))
            OR (connection.definition_key IN ('sentry-organization','sentry-self-hosted-organization')
             AND invocation.resource_kind='HTTPS_RESOURCE' AND invocation.operation=invocation.capability_key
             AND binding.verified_capabilities @> ARRAY['sentry.organization.read']::text[]
             AND NOT binding.denied_capabilities && ARRAY['sentry.organization.read']::text[]
             AND (
               (NEW.resource_permission_profile='SENTRY_ORGANIZATION_OBJECT_READ_V1'
                AND invocation.risk='READ' AND invocation.approval_policy='NONE'
                AND invocation.capability_key IN ('sentry.organization.read','sentry.organization.project.list',
                  'sentry.organization.release.list','sentry.organization.release.read','sentry.organization.alert.list',
                  'sentry.organization.alert.read','sentry.organization.detector.list','sentry.organization.detector.read'))
               OR (NEW.resource_permission_profile='SENTRY_ORGANIZATION_MUTATION_V1'
                AND invocation.approval_policy='HUMAN_EACH_EFFECT'
                AND ((invocation.risk='WRITE' AND invocation.capability_key IN ('sentry.organization.release.create',
                  'sentry.organization.release.update','sentry.organization.alert.create','sentry.organization.alert.update'))
                  OR (invocation.risk='DESTRUCTIVE' AND invocation.capability_key='sentry.organization.alert.delete'))
                AND binding.observed_permission_profiles @> ARRAY['SENTRY_TOKEN_SCOPES_V1']::text[])
             ))
            OR (connection.definition_key IN ('prometheus','thanos')
             AND NEW.resource_permission_profile='OBSERVABILITY_TYPED_RESOURCE_READ_V1'
             AND invocation.resource_kind='HTTPS_RESOURCE' AND invocation.operation=invocation.capability_key
             AND invocation.risk='READ' AND invocation.approval_policy='NONE'
             AND invocation.capability_key IN (connection.definition_key||'.connection.read',connection.definition_key||'.query.instant',
              connection.definition_key||'.query.range',connection.definition_key||'.labels.list',connection.definition_key||'.label.values',
              connection.definition_key||'.series.list',connection.definition_key||'.rules.list',connection.definition_key||'.alerts.list')
             AND binding.verified_capabilities @> ARRAY[connection.definition_key||'.connection.read']::text[]
             AND NOT binding.denied_capabilities && ARRAY[connection.definition_key||'.connection.read']::text[])
            OR (connection.definition_key IN ('grafana-dashboard','grafana-organization','grafana-dashboard-annotations','grafana-organization-rule-delete')
             AND invocation.resource_kind='HTTPS_RESOURCE' AND invocation.operation=invocation.capability_key
             AND binding.verified_capabilities @> ARRAY['grafana.connection.read']::text[]
             AND NOT binding.denied_capabilities && ARRAY['grafana.connection.read']::text[]
             AND (
              (NEW.resource_permission_profile='OBSERVABILITY_TYPED_RESOURCE_READ_V1'
               AND invocation.risk='READ' AND invocation.approval_policy='NONE'
               AND (invocation.capability_key='grafana.connection.read'
                OR (connection.definition_key IN ('grafana-dashboard','grafana-organization')
                 AND invocation.capability_key IN ('grafana.datasource.read','grafana.datasource.query'))
                OR (connection.definition_key IN ('grafana-dashboard','grafana-dashboard-annotations')
                 AND invocation.capability_key='grafana.dashboard.read')
                OR (connection.definition_key IN ('grafana-dashboard','grafana-organization','grafana-dashboard-annotations')
                 AND invocation.capability_key IN ('grafana.annotation.list','grafana.annotation.read'))
                OR (connection.definition_key IN ('grafana-organization','grafana-organization-rule-delete')
                 AND invocation.capability_key IN ('grafana.rule.list','grafana.rule.read'))
                OR (connection.definition_key='grafana-organization'
                 AND invocation.capability_key IN ('grafana.silence.list','grafana.silence.read'))))
              OR (NEW.resource_permission_profile='GRAFANA_TYPED_WRITE_V1'
               AND invocation.approval_policy='HUMAN_EACH_EFFECT'
               AND binding.observed_permission_profiles @> ARRAY['GRAFANA_CURRENT_PERMISSIONS_V1']::text[]
               AND (
                (connection.definition_key IN ('grafana-organization','grafana-dashboard-annotations')
                 AND ((invocation.risk='WRITE' AND invocation.capability_key IN ('grafana.annotation.create','grafana.annotation.update'))
                  OR (invocation.risk='DESTRUCTIVE' AND invocation.capability_key='grafana.annotation.delete')))
                OR (connection.definition_key='grafana-organization'
                 AND ((invocation.risk='WRITE' AND invocation.capability_key IN ('grafana.silence.create','grafana.silence.update','grafana.rule.create','grafana.rule.update'))
                  OR (invocation.risk='DESTRUCTIVE' AND invocation.capability_key='grafana.silence.delete')))
                OR (connection.definition_key='grafana-organization-rule-delete'
                 AND invocation.risk='DESTRUCTIVE' AND invocation.capability_key='grafana.rule.delete')
               ))
             ))
            OR (connection.definition_key IN ('loki','loki-ruler-namespace-read','loki-ruler-namespace')
             AND invocation.resource_kind='HTTPS_RESOURCE' AND invocation.operation=invocation.capability_key
             AND binding.verified_capabilities @> ARRAY['loki.connection.read']::text[]
             AND NOT binding.denied_capabilities && ARRAY['loki.connection.read']::text[]
             AND (
              (NEW.resource_permission_profile='OBSERVABILITY_TYPED_RESOURCE_READ_V1'
               AND invocation.risk='READ' AND invocation.approval_policy='NONE'
               AND (invocation.capability_key='loki.connection.read'
                OR (connection.definition_key='loki' AND invocation.capability_key IN ('loki.query.instant','loki.query.range','loki.logs.query','loki.labels.list','loki.label.values','loki.series.list'))
                OR (connection.definition_key IN ('loki-ruler-namespace-read','loki-ruler-namespace') AND invocation.capability_key IN ('loki.ruler.group.list','loki.ruler.group.read'))))
              OR (NEW.resource_permission_profile='LOKI_RULER_MUTATION_V1'
               AND connection.definition_key='loki-ruler-namespace' AND invocation.approval_policy='HUMAN_EACH_EFFECT'
               AND ((invocation.risk='WRITE' AND invocation.capability_key='loki.ruler.group.replace')
                OR (invocation.risk='DESTRUCTIVE' AND invocation.capability_key='loki.ruler.group.delete')))
             ))
            OR (NEW.resource_permission_profile='GITLAB_SCOPED_RESOURCE_V2'$after$;
 empty_marker text := $empty$(NEW.resource_permission_profile=''$empty$;
 empty_replacement text := $empty$(NEW.resource_permission_profile='' AND connection.definition_key NOT IN ('sentry','sentry-self-hosted','sentry-organization','sentry-self-hosted-organization','prometheus','thanos','grafana-dashboard','grafana-organization','grafana-dashboard-annotations','grafana-organization-rule-delete','loki','loki-ruler-namespace-read','loki-ruler-namespace')$empty$;
BEGIN
 IF NOT EXISTS (SELECT 1 FROM pg_proc WHERE oid='control_plane.guard_personal_invocation_credential()'::regprocedure
  AND pg_get_userbyid(proowner)='control_plane_owner' AND NOT prosecdef)
 THEN RAISE EXCEPTION 'Sentry credential guard owner changed'; END IF;
 SELECT pg_get_functiondef('control_plane.guard_personal_invocation_credential()'::regprocedure) INTO source;
 IF (length(source)-length(replace(source,marker,'')))/length(marker)<>1
  OR (length(source)-length(replace(source,empty_marker,'')))/length(empty_marker)<>1
  OR position('GITHUB_SCOPED_RESOURCE_V1' IN source)=0
  OR position('SENTRY_PROJECT_OBJECT_READ_V1' IN source)>0
  OR position('integration_declared_policy_allowed(connection.id,invocation.capability_key,invocation.operation,' IN source)=0
 THEN RAISE EXCEPTION 'Sentry credential guard predecessor changed'; END IF;
 source:=replace(source,marker,replacement);
 source:=replace(source,empty_marker,empty_replacement);
 EXECUTE source;
END $sentry_guard$;
-- +goose StatementEnd
SET LOCAL search_path=public;
RESET ROLE;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Sentry personal credential migration is forward-only'; END $$;
-- +goose StatementEnd
