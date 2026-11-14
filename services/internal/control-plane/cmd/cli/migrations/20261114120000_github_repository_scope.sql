-- +goose Up
SET ROLE control_plane_owner;
SET LOCAL search_path=pg_catalog;
-- BEGIN GENERATED GITHUB SCOPE PREDECESSOR
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
IF actual IS DISTINCT FROM $normal${"authorityDigest":"9078371cb08839694c21e3d8750e8da1d309c6f45616662acbbc8d72eb0417eb","catalogDigest":"e1397db387f27772deb3a5c04e3c9d52dab4965ee4443e330db5f59e8c04e343","functionCount":224,"functionsDigest":"9f80e737b658516ce1c6854916a642d9fbebfc8367805aebe2555fa4b5c77864","purgeEdgeCount":310,"purgeEdgeFingerprint":"2f32d3b42715f0a7c905ddc9f6957e71","purgeGraphDigest":"3bb0a85fbf658cf7cdb6af06a76f2e0c6328560bcdc63c459d5185cb174aba84","purgeNodeCount":112,"purgeNodeFingerprint":"97039cc15f63901977000dabe1167d5d","tableCount":168,"tableDefaultACL":["{control_plane_runtime=arwd/control_plane_owner}"],"tablesDigest":"626fe142f5099ac0962f52b161863fd1f2395847bfa3db92196cdc3a7bc5cf24","triggerCount":178,"triggersDigest":"59f763ccc389521692e8989e0d03cf4308576488851a669c618ededd61113342"}$normal$::jsonb AND actual IS DISTINCT FROM $backup${"authorityDigest":"7c9c625617acd100ce4d5fe8c29722854d1083bf30f65aef0043a490f3dec03c","catalogDigest":"2e7e1464e0470461172e4abf77e773e933ad32668e117b69f291efdeb732e27d","functionCount":224,"functionsDigest":"9f80e737b658516ce1c6854916a642d9fbebfc8367805aebe2555fa4b5c77864","purgeEdgeCount":310,"purgeEdgeFingerprint":"2f32d3b42715f0a7c905ddc9f6957e71","purgeGraphDigest":"3bb0a85fbf658cf7cdb6af06a76f2e0c6328560bcdc63c459d5185cb174aba84","purgeNodeCount":112,"purgeNodeFingerprint":"97039cc15f63901977000dabe1167d5d","tableCount":168,"tableDefaultACL":["{control_plane_runtime=arwd/control_plane_owner,kodex_backup_reader=r/control_plane_owner}"],"tablesDigest":"78305c44dde2a65bf9aa71e7f2b5c3b847d23874f28ef789de8117485c2ed6be","triggerCount":178,"triggersDigest":"59f763ccc389521692e8989e0d03cf4308576488851a669c618ededd61113342"}$backup$::jsonb THEN RAISE EXCEPTION 'GitHub scope exact predecessor catalog changed';END IF;
END $closed_predecessor$;
-- +goose StatementEnd
-- END GENERATED GITHUB SCOPE PREDECESSOR

-- Discovery — безопасное наблюдение собственного ключа, не новый источник авторизации.
ALTER TABLE control_plane.integration_personal_credentials ADD COLUMN github_discovery jsonb
 CHECK (github_discovery IS NULL OR state='READY' AND jsonb_typeof(github_discovery)='object' AND octet_length(github_discovery::text)<=65536);
ALTER TABLE control_plane.integration_personal_credential_tests ADD COLUMN github_discovery jsonb
 CHECK (github_discovery IS NULL OR state='SUCCEEDED' AND jsonb_typeof(github_discovery)='object' AND octet_length(github_discovery::text)<=65536);

-- +goose StatementBegin
CREATE FUNCTION control_plane.clear_personal_github_discovery() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,control_plane AS $clear$
BEGIN
 IF NEW.state<>'READY' THEN NEW.github_discovery:=NULL; END IF;
 RETURN NEW;
END;
$clear$;
-- +goose StatementEnd
CREATE TRIGGER aa_personal_github_discovery_clear BEFORE INSERT OR UPDATE ON control_plane.integration_personal_credentials
 FOR EACH ROW EXECUTE FUNCTION control_plane.clear_personal_github_discovery();
REVOKE ALL ON FUNCTION control_plane.clear_personal_github_discovery() FROM PUBLIC;

-- Личная ревизия хранится отдельно от общей; обе nullable колонки взаимоисключающие.
ALTER TABLE control_plane.repository_bundle_publications
 ALTER COLUMN credential_revision_id DROP NOT NULL,
 ADD COLUMN personal_credential_revision_id uuid REFERENCES control_plane.integration_personal_credential_revisions(id) DEFERRABLE INITIALLY IMMEDIATE,
 ADD CONSTRAINT repository_bundle_credential_selection_check CHECK ((credential_revision_id IS NULL)<>(personal_credential_revision_id IS NULL));

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_repository_bundle_personal_credential() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,control_plane AS $bundle_credential$
BEGIN
 IF TG_OP='UPDATE' THEN
  IF NEW.personal_credential_revision_id IS DISTINCT FROM OLD.personal_credential_revision_id THEN RAISE EXCEPTION 'repository bundle personal credential is immutable';END IF;
  RETURN NEW;
 END IF;
 IF NEW.personal_credential_revision_id IS NOT NULL THEN
  IF NOT EXISTS(SELECT 1 FROM control_plane.integration_personal_invocation_credentials pin
   JOIN control_plane.integration_personal_credential_revisions revision ON revision.id=pin.credential_revision_id AND revision.binding_id=pin.binding_id AND revision.organization_id=pin.organization_id
   JOIN control_plane.runs root ON root.id=pin.root_run_id AND root.organization_id=pin.organization_id
   WHERE pin.invocation_id=NEW.integration_invocation_id AND pin.organization_id=NEW.organization_id
    AND pin.connection_id=NEW.connection_id AND pin.subject_id=NEW.root_actor_id AND root.initiated_by=pin.subject_id
    AND pin.root_run_id=NEW.root_run_id AND revision.id=NEW.personal_credential_revision_id
    AND pin.resource_permission_profile='GITHUB_SCOPED_RESOURCE_V1') THEN RAISE EXCEPTION 'repository bundle personal credential lineage is invalid';END IF;
 ELSIF NOT EXISTS(SELECT 1 FROM control_plane.integration_credential_revisions revision
  JOIN control_plane.integration_connections connection ON connection.id=revision.connection_id AND connection.organization_id=revision.organization_id
  WHERE revision.id=NEW.credential_revision_id AND revision.organization_id=NEW.organization_id AND revision.connection_id=NEW.connection_id AND connection.credential_policy='PROJECT_SHARED') THEN
  RAISE EXCEPTION 'repository bundle shared credential lineage is invalid';
 END IF;
 RETURN NEW;
END;
$bundle_credential$;
-- +goose StatementEnd
CREATE TRIGGER repository_bundle_personal_credential_guard BEFORE INSERT OR UPDATE ON control_plane.repository_bundle_publications
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_repository_bundle_personal_credential();
REVOKE ALL ON FUNCTION control_plane.guard_repository_bundle_personal_credential() FROM PUBLIC;

-- Закрытые CHECK расширяются только одним известным значением, прочие профили сохраняются.
-- +goose StatementBegin
DO $scope_checks$
DECLARE target regclass; constraint_name text; definition text; predecessor text; successor text;
BEGIN
 FOREACH target IN ARRAY ARRAY['control_plane.integration_grants'::regclass,'control_plane.integration_invocations'::regclass,'control_plane.integration_personal_invocation_credentials'::regclass] LOOP
  IF target='control_plane.integration_personal_invocation_credentials'::regclass THEN
   constraint_name:='integration_personal_invocati_resource_permission_profile_check';
   predecessor:='''GITLAB_SCOPED_RESOURCE_V2''::text';successor:='''GITLAB_SCOPED_RESOURCE_V2''::text, ''GITHUB_SCOPED_RESOURCE_V1''::text';
  ELSE
   IF target='control_plane.integration_grants'::regclass THEN constraint_name:='integration_grants_resource_kind_check';ELSE constraint_name:='integration_invocations_resource_kind_check';END IF;
   predecessor:='''GITLAB_SCOPE''::text';successor:='''GITLAB_SCOPE''::text, ''GITHUB_SCOPE''::text';
  END IF;
  SELECT pg_get_constraintdef(oid) INTO STRICT definition FROM pg_constraint WHERE conrelid=target AND conname=constraint_name AND contype='c' AND convalidated;
  IF (length(definition)-length(replace(definition,predecessor,'')))/length(predecessor)<>1 THEN RAISE EXCEPTION 'GitHub scope constraint predecessor changed';END IF;
  EXECUTE format('ALTER TABLE %s DROP CONSTRAINT %I, ADD CONSTRAINT %I %s',target,constraint_name,constraint_name,replace(definition,predecessor,successor));
 END LOOP;
END $scope_checks$;
-- +goose StatementEnd

-- Fresh identity является prerequisite, остальные права подтверждает конкретный per-call consumer.
-- +goose StatementBegin
DO $personal_github$
DECLARE source text; previous text:=$before$(NEW.resource_permission_profile='GITLAB_SCOPED_RESOURCE_V2'$before$;
 successor text:=$after$(NEW.resource_permission_profile='GITHUB_SCOPED_RESOURCE_V1'
             AND connection.definition_key='github' AND invocation.resource_kind='GITHUB_SCOPE'
             AND invocation.operation=invocation.capability_key AND invocation.capability_key<>'github.identity.read'
             AND binding.verified_capabilities @> ARRAY['github.identity.read']::text[]
             AND NOT binding.denied_capabilities && ARRAY['github.identity.read']::text[])
            OR (NEW.resource_permission_profile='GITLAB_SCOPED_RESOURCE_V2'$after$;
 empty_before text:=$empty$(NEW.resource_permission_profile=''
             AND NOT(connection.definition_key='gitlab'$empty$;
 empty_after text:=$empty$(NEW.resource_permission_profile=''
             AND NOT(connection.definition_key='github' AND invocation.resource_kind='GITHUB_SCOPE' AND invocation.capability_key<>'github.identity.read')
             AND NOT(connection.definition_key='gitlab'$empty$;
BEGIN
 SELECT pg_get_functiondef('control_plane.guard_personal_invocation_credential()'::regprocedure) INTO source;
 IF (length(source)-length(replace(source,previous,'')))/length(previous)<>1 OR (length(source)-length(replace(source,empty_before,'')))/length(empty_before)<>1
 THEN RAISE EXCEPTION 'GitHub personal credential predecessor changed';END IF;
 EXECUTE replace(replace(source,previous,successor),empty_before,empty_after);
 IF position('GITHUB_SCOPED_RESOURCE_V1' in pg_get_functiondef('control_plane.guard_personal_invocation_credential()'::regprocedure))=0 THEN RAISE EXCEPTION 'GitHub personal credential readback failed';END IF;
END $personal_github$;
-- +goose StatementEnd

-- Exact исторические пакеты6.0–6.4 сохраняют прежние политики; новая область им не выдаётся.
-- +goose StatementBegin
DO $github_history$
DECLARE source text; old_current text:='''github'',''6.4.0'',''bd6d504e4fc1d19d5c083f8585115c04ec75852b7879ad19ed54dc41cbe9a1da''';
 new_current text:='''github'',''6.5.0'',''6a404eba08a820759d68b67e44eef1a3750743f6505491428b56e5d979340545''';
 insertion text:=$case$ WHEN (p_key,p_current_version,p_current_digest,p_version,p_digest)=('github','6.5.0','6a404eba08a820759d68b67e44eef1a3750743f6505491428b56e5d979340545','6.4.0','bd6d504e4fc1d19d5c083f8585115c04ec75852b7879ad19ed54dc41cbe9a1da') THEN $caps$[{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"NONE","approvalScope":{"name":"Область подключения","requiredInputPaths":[],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.repository.metadata.read","name":"Прочитать метаданные репозитория","operation":"github.repository.metadata.read","resourceKind":"GITHUB_REPOSITORY","risk":"READ"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"HUMAN_EACH_EFFECT","approvalScope":{"name":"Область подключения","requiredInputPaths":[],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.issue.create","name":"Создать issue","operation":"github.issue.create","resourceKind":"GITHUB_REPOSITORY","risk":"WRITE"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"NONE","approvalScope":{"name":"Область подключения","requiredInputPaths":[],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.issue.list","name":"Получить список issue","operation":"github.issue.list","resourceKind":"GITHUB_REPOSITORY","risk":"READ"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"NONE","approvalScope":{"name":"Задача","requiredInputPaths":["/issue_number"],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.issue.read","name":"Прочитать issue","operation":"github.issue.read","resourceKind":"GITHUB_REPOSITORY","risk":"READ"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"HUMAN_EACH_EFFECT","approvalScope":{"name":"Задача","requiredInputPaths":["/issue_number"],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.issue.comment.create","name":"Добавить комментарий к issue","operation":"github.issue.comment.create","resourceKind":"GITHUB_REPOSITORY","risk":"WRITE"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"HUMAN_EACH_EFFECT","approvalScope":{"name":"Задача","requiredInputPaths":["/issue_number"],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.issue.update","name":"Обновить issue","operation":"github.issue.update","resourceKind":"GITHUB_REPOSITORY","risk":"WRITE"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"NONE","approvalScope":{"name":"Репозиторий и файл или ветка","requiredInputPaths":["/path","/ref"],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.repository.content.list","name":"Получить содержимое каталога","operation":"github.repository.content.list","resourceKind":"GITHUB_REPOSITORY","risk":"READ"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"NONE","approvalScope":{"name":"Репозиторий и файл или ветка","requiredInputPaths":["/path","/ref"],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.repository.content.read","name":"Прочитать файл","operation":"github.repository.content.read","resourceKind":"GITHUB_REPOSITORY","risk":"READ"},{"allowedApprovalPolicies":["HUMAN_EACH_EFFECT"],"approvalPolicy":"HUMAN_EACH_EFFECT","key":"github.repository.bundle.publish","name":"Опубликовать bundle исходников","operation":"github.repository.bundle.publish","resourceKind":"GITHUB_REPOSITORY","risk":"WRITE"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"HUMAN_EACH_EFFECT","approvalScope":{"name":"Репозиторий и файл или ветка","requiredInputPaths":["/path","/branch"],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.repository.content.create","name":"Создать файл","operation":"github.repository.content.create","resourceKind":"GITHUB_REPOSITORY","risk":"WRITE"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"HUMAN_EACH_EFFECT","approvalScope":{"name":"Репозиторий и файл или ветка","requiredInputPaths":["/path","/branch"],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.repository.content.update","name":"Обновить файл","operation":"github.repository.content.update","resourceKind":"GITHUB_REPOSITORY","risk":"WRITE"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"HUMAN_EACH_EFFECT","approvalScope":{"name":"Репозиторий и файл или ветка","requiredInputPaths":["/path","/branch"],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.repository.content.delete","name":"Удалить файл","operation":"github.repository.content.delete","resourceKind":"GITHUB_REPOSITORY","risk":"DESTRUCTIVE"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"NONE","approvalScope":{"name":"Область подключения","requiredInputPaths":[],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.branch.list","name":"Получить ветки","operation":"github.branch.list","resourceKind":"GITHUB_REPOSITORY","risk":"READ"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"NONE","approvalScope":{"name":"Репозиторий и файл или ветка","requiredInputPaths":["/branch"],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.branch.read","name":"Прочитать ветку","operation":"github.branch.read","resourceKind":"GITHUB_REPOSITORY","risk":"READ"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"HUMAN_EACH_EFFECT","approvalScope":{"name":"Репозиторий и файл или ветка","requiredInputPaths":["/branch"],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.branch.create","name":"Создать ветку","operation":"github.branch.create","resourceKind":"GITHUB_REPOSITORY","risk":"WRITE"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"HUMAN_EACH_EFFECT","approvalScope":{"name":"Репозиторий и файл или ветка","requiredInputPaths":["/branch"],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.branch.delete","name":"Удалить ветку","operation":"github.branch.delete","resourceKind":"GITHUB_REPOSITORY","risk":"DESTRUCTIVE"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"NONE","approvalScope":{"name":"Область подключения","requiredInputPaths":[],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.commit.list","name":"Получить коммиты","operation":"github.commit.list","resourceKind":"GITHUB_REPOSITORY","risk":"READ"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"NONE","approvalScope":{"name":"Область подключения","requiredInputPaths":[],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.commit.read","name":"Прочитать коммит","operation":"github.commit.read","resourceKind":"GITHUB_REPOSITORY","risk":"READ"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"NONE","approvalScope":{"name":"Область подключения","requiredInputPaths":[],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.pull_request.list","name":"Получить pull requests","operation":"github.pull_request.list","resourceKind":"GITHUB_REPOSITORY","risk":"READ"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"NONE","approvalScope":{"name":"Pull request","requiredInputPaths":["/pull_request_number"],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.pull_request.read","name":"Прочитать pull request","operation":"github.pull_request.read","resourceKind":"GITHUB_REPOSITORY","risk":"READ"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"HUMAN_EACH_EFFECT","approvalScope":{"name":"Исходная и целевая ветки","requiredInputPaths":["/head","/base"],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.pull_request.create","name":"Создать pull request","operation":"github.pull_request.create","resourceKind":"GITHUB_REPOSITORY","risk":"WRITE"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"HUMAN_EACH_EFFECT","approvalScope":{"name":"Pull request","requiredInputPaths":["/pull_request_number"],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.pull_request.update","name":"Обновить pull request","operation":"github.pull_request.update","resourceKind":"GITHUB_REPOSITORY","risk":"WRITE"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"HUMAN_EACH_EFFECT","approvalScope":{"name":"Pull request","requiredInputPaths":["/pull_request_number"],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.pull_request.merge","name":"Слить pull request","operation":"github.pull_request.merge","resourceKind":"GITHUB_REPOSITORY","risk":"DESTRUCTIVE"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"NONE","approvalScope":{"name":"Pull request","requiredInputPaths":["/pull_request_number"],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.pull_request.review.list","name":"Получить reviews","operation":"github.pull_request.review.list","resourceKind":"GITHUB_REPOSITORY","risk":"READ"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"NONE","approvalScope":{"name":"Pull request","requiredInputPaths":["/pull_request_number","/review_id"],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.pull_request.review.read","name":"Прочитать review","operation":"github.pull_request.review.read","resourceKind":"GITHUB_REPOSITORY","risk":"READ"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"HUMAN_EACH_EFFECT","approvalScope":{"name":"Pull request, commit и тип review","requiredInputPaths":["/pull_request_number","/event"],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.pull_request.review.create","name":"Создать review","operation":"github.pull_request.review.create","resourceKind":"GITHUB_REPOSITORY","risk":"WRITE"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"NONE","approvalScope":{"name":"Задача","requiredInputPaths":["/issue_number"],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.issue.comment.list","name":"Получить комментарии issue","operation":"github.issue.comment.list","resourceKind":"GITHUB_REPOSITORY","risk":"READ"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"NONE","approvalScope":{"name":"Задача","requiredInputPaths":["/issue_number","/comment_id"],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.issue.comment.read","name":"Прочитать комментарий issue","operation":"github.issue.comment.read","resourceKind":"GITHUB_REPOSITORY","risk":"READ"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"HUMAN_EACH_EFFECT","approvalScope":{"name":"Задача","requiredInputPaths":["/issue_number","/comment_id"],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.issue.comment.update","name":"Обновить комментарий issue","operation":"github.issue.comment.update","resourceKind":"GITHUB_REPOSITORY","risk":"WRITE"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"HUMAN_EACH_EFFECT","approvalScope":{"name":"Задача","requiredInputPaths":["/issue_number","/comment_id"],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.issue.comment.delete","name":"Удалить комментарий issue","operation":"github.issue.comment.delete","resourceKind":"GITHUB_REPOSITORY","risk":"DESTRUCTIVE"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"NONE","approvalScope":{"name":"Область подключения","requiredInputPaths":[],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.check_run.list","name":"Получить checks","operation":"github.check_run.list","resourceKind":"GITHUB_REPOSITORY","risk":"READ"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"NONE","approvalScope":{"name":"Область подключения","requiredInputPaths":["/check_run_id"],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.check_run.read","name":"Прочитать check","operation":"github.check_run.read","resourceKind":"GITHUB_REPOSITORY","risk":"READ"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"NONE","approvalScope":{"name":"Область подключения","requiredInputPaths":[],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.actions.workflow.list","name":"Получить workflows","operation":"github.actions.workflow.list","resourceKind":"GITHUB_REPOSITORY","risk":"READ"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"NONE","approvalScope":{"name":"CI workflow или job","requiredInputPaths":["/workflow_id"],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.actions.workflow.read","name":"Прочитать workflow","operation":"github.actions.workflow.read","resourceKind":"GITHUB_REPOSITORY","risk":"READ"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"HUMAN_EACH_EFFECT","approvalScope":{"name":"CI workflow или job","requiredInputPaths":["/workflow_id"],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.actions.workflow.dispatch","name":"Запустить workflow","operation":"github.actions.workflow.dispatch","resourceKind":"GITHUB_REPOSITORY","risk":"SENSITIVE"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"NONE","approvalScope":{"name":"Область подключения","requiredInputPaths":[],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.actions.run.list","name":"Получить workflow runs","operation":"github.actions.run.list","resourceKind":"GITHUB_REPOSITORY","risk":"READ"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"NONE","approvalScope":{"name":"CI workflow или job","requiredInputPaths":["/run_id"],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.actions.run.read","name":"Прочитать workflow run","operation":"github.actions.run.read","resourceKind":"GITHUB_REPOSITORY","risk":"READ"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"HUMAN_EACH_EFFECT","approvalScope":{"name":"CI workflow или job","requiredInputPaths":["/run_id"],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.actions.run.rerun","name":"Повторно запустить workflow run","operation":"github.actions.run.rerun","resourceKind":"GITHUB_REPOSITORY","risk":"SENSITIVE"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"HUMAN_EACH_EFFECT","approvalScope":{"name":"CI workflow или job","requiredInputPaths":["/run_id"],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.actions.run.cancel","name":"Отменить workflow run","operation":"github.actions.run.cancel","resourceKind":"GITHUB_REPOSITORY","risk":"DESTRUCTIVE"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"NONE","approvalScope":{"name":"CI workflow или job","requiredInputPaths":["/run_id"],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.actions.job.list","name":"Получить jobs запуска","operation":"github.actions.job.list","resourceKind":"GITHUB_REPOSITORY","risk":"READ"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"NONE","approvalScope":{"name":"CI workflow или job","requiredInputPaths":["/run_id","/job_id"],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.actions.job.read","name":"Прочитать job","operation":"github.actions.job.read","resourceKind":"GITHUB_REPOSITORY","risk":"READ"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"NONE","approvalScope":{"name":"Pull request","requiredInputPaths":["/pull_request_number"],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.pull_request.file.list","name":"Получить индекс изменённых файлов pull request","operation":"github.pull_request.file.list","resourceKind":"GITHUB_REPOSITORY","risk":"READ"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"HUMAN_EACH_EFFECT","approvalScope":{"name":"Pull request","requiredInputPaths":["/pull_request_number"],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.pull_request.comment.create","name":"Добавить комментарий к pull request","operation":"github.pull_request.comment.create","resourceKind":"GITHUB_REPOSITORY","risk":"WRITE"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"NONE","approvalScope":{"name":"Pull request","requiredInputPaths":["/pull_request_number"],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.pull_request.comment.list","name":"Получить комментарии pull request","operation":"github.pull_request.comment.list","resourceKind":"GITHUB_REPOSITORY","risk":"READ"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"NONE","approvalScope":{"name":"Pull request","requiredInputPaths":["/pull_request_number","/comment_id"],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.pull_request.comment.read","name":"Прочитать комментарий pull request","operation":"github.pull_request.comment.read","resourceKind":"GITHUB_REPOSITORY","risk":"READ"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"HUMAN_EACH_EFFECT","approvalScope":{"name":"Pull request","requiredInputPaths":["/pull_request_number","/comment_id"],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.pull_request.comment.update","name":"Обновить комментарий pull request","operation":"github.pull_request.comment.update","resourceKind":"GITHUB_REPOSITORY","risk":"WRITE"},{"allowedApprovalPolicies":["NONE","HUMAN_EACH_EFFECT","HUMAN_SCOPED"],"approvalPolicy":"HUMAN_EACH_EFFECT","approvalScope":{"name":"Pull request","requiredInputPaths":["/pull_request_number","/expected_head_sha"],"limits":{"durationSeconds":604800,"maxEffects":10000},"allowedLifetimes":["ROOT_RUN","INITIATOR_RESOURCE"]},"key":"github.pull_request.ready","name":"Передать pull request на ревью","operation":"github.pull_request.ready","resourceKind":"GITHUB_REPOSITORY","risk":"WRITE"}]$caps$::jsonb
$case$;
BEGIN
 SELECT pg_get_functiondef('control_plane.integration_policy_legacy_capabilities(text,text,text,text,text)'::regprocedure) INTO source;
 IF (length(source)-length(replace(source,old_current,'')))/length(old_current)<>4 OR (length(source)-length(replace(source,'SELECT CASE','')))/length('SELECT CASE')<>1 THEN RAISE EXCEPTION 'GitHub historical package predecessor changed';END IF;
 source:=replace(source,old_current,new_current);
 EXECUTE replace(source,'SELECT CASE','SELECT CASE'||E'\n'||insertion);
END $github_history$;
-- +goose StatementEnd

-- Точная проекция закреплённого invocation scope для прежних workflow evidence parsers.
-- Locator задаёт данные, а producing invocation, root и receipt сохраняют полномочия.
-- +goose StatementBegin
CREATE FUNCTION control_plane.workflow_github_repository(p_scope jsonb,p_input jsonb)
RETURNS text LANGUAGE plpgsql IMMUTABLE SECURITY INVOKER SET search_path=pg_catalog AS $$
DECLARE v_id numeric;v_name text;v_repositories jsonb;
BEGIN
 IF NOT (p_scope ? 'scope_kind') THEN
  IF jsonb_typeof(p_scope)='object' AND p_scope-ARRAY['owner','repository']='{}'::jsonb AND p_scope ?& ARRAY['owner','repository']
   AND jsonb_typeof(p_scope->'owner')='string' AND jsonb_typeof(p_scope->'repository')='string' THEN RETURN (p_scope->>'owner')||'/'||(p_scope->>'repository'); END IF;
  RETURN NULL;
 END IF;
 IF jsonb_typeof(p_scope) IS DISTINCT FROM 'object' OR jsonb_typeof(p_input) IS DISTINCT FROM 'object'
  OR jsonb_typeof(p_input->'repository_id') IS DISTINCT FROM 'number' OR jsonb_typeof(p_input->'repository_full_name') IS DISTINCT FROM 'string' THEN RETURN NULL;END IF;
 v_id:=(p_input->>'repository_id')::numeric;v_name:=p_input->>'repository_full_name';
 IF v_id<1 OR v_id>9007199254740991 OR trunc(v_id)<>v_id OR v_name !~ '^[A-Za-z0-9_.-]{1,100}/[A-Za-z0-9_.-]{1,100}$'
  OR split_part(v_name,'/',1) IN('.','..') OR split_part(v_name,'/',2) IN('.','..') THEN RETURN NULL;END IF;
 IF p_scope->>'scope_kind'='ALL_ACCESSIBLE' AND p_scope-ARRAY['scope_kind']='{}'::jsonb THEN RETURN v_name;END IF;
 IF p_scope->>'scope_kind' IS DISTINCT FROM 'SELECTED' OR p_scope-ARRAY['scope_kind','repositories']<>'{}'::jsonb
  OR jsonb_typeof(p_scope->'repositories') IS DISTINCT FROM 'string' OR octet_length(p_scope->>'repositories')>32768 THEN RETURN NULL;END IF;
 BEGIN v_repositories:=(p_scope->>'repositories')::jsonb;EXCEPTION WHEN data_exception THEN RETURN NULL;END;
 IF jsonb_typeof(v_repositories) IS DISTINCT FROM 'array' OR jsonb_array_length(v_repositories) NOT BETWEEN 1 AND 100
  OR EXISTS(SELECT 1 FROM jsonb_array_elements(v_repositories) item WHERE jsonb_typeof(item) IS DISTINCT FROM 'object' OR item-ARRAY['id','full_name']<>'{}'::jsonb
    OR jsonb_typeof(item->'id') IS DISTINCT FROM 'number' OR jsonb_typeof(item->'full_name') IS DISTINCT FROM 'string') THEN RETURN NULL;END IF;
 IF (SELECT count(*) FROM jsonb_array_elements(v_repositories) item WHERE item->'id'=p_input->'repository_id' AND item->>'full_name'=v_name)=1 THEN RETURN v_name;END IF;
 RETURN NULL;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION control_plane.workflow_personal_invocation_ready(p_invocation uuid)
RETURNS boolean LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $$
 SELECT EXISTS(SELECT 1 FROM control_plane.integration_invocations invocation
 JOIN control_plane.integration_connections connection ON connection.id=invocation.connection_id AND connection.organization_id=invocation.organization_id
 JOIN control_plane.integration_personal_invocation_credentials pin ON pin.invocation_id=invocation.id AND pin.organization_id=invocation.organization_id AND pin.connection_id=connection.id
 JOIN control_plane.runs execution ON execution.id=invocation.run_id AND execution.organization_id=invocation.organization_id
 JOIN control_plane.runs root ON root.id=execution.root_run_id AND root.organization_id=execution.organization_id AND root.root_run_id=root.id AND root.initiated_by=pin.subject_id AND root.id=pin.root_run_id
 JOIN control_plane.subjects subject ON subject.id=root.initiated_by AND subject.organization_id=root.organization_id AND subject.active
 JOIN control_plane.integration_personal_credentials binding ON binding.id=pin.binding_id AND binding.organization_id=pin.organization_id AND binding.connection_id=connection.id AND binding.subject_id=pin.subject_id
  AND binding.generation=pin.binding_generation AND binding.credential_revision_id=pin.credential_revision_id AND binding.state='READY'
  AND binding.verified_credential_revision_id=pin.credential_revision_id AND binding.verified_definition_digest=pin.definition_digest AND binding.verified_configuration_digest=pin.configuration_digest
  AND (binding.expires_at IS NULL OR binding.expires_at>statement_timestamp())
 WHERE invocation.id=p_invocation AND connection.credential_policy='INITIATOR_REQUIRED' AND connection.lifecycle_state='ACTIVE' AND connection.enabled
  AND connection.definition_digest=pin.definition_digest AND invocation.definition_digest=pin.definition_digest
  AND pin.resource_permission_profile='GITHUB_SCOPED_RESOURCE_V1' AND invocation.resource_kind='GITHUB_SCOPE'
  AND control_plane.integration_connection_configuration_ready(connection.id));
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.workflow_github_repository(jsonb,jsonb),control_plane.workflow_personal_invocation_ready(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.workflow_github_repository(jsonb,jsonb),control_plane.workflow_personal_invocation_ready(uuid) TO control_plane_runtime;

-- Каждое существующее объявление сохраняет OID/ACL, INVOKER, volatility и ABI.
-- Полный predecessor catalog отдельно закреплён immutable proof044.
-- +goose StatementBegin
DO $workflow_scope$
DECLARE item record;source text;original text;name text;needle text;replacement text;v_count integer;
BEGIN
 FOR item IN SELECT oid FROM pg_proc WHERE pronamespace='control_plane'::regnamespace AND proname IN(
  'guard_workflow_effect_records','workflow_evidence_intent_valid','workflow_evidence_outcome_valid',
  'workflow_pdlc_intent_valid','workflow_receipt_effect_proof','workflow_receipt_lineage_proof') LOOP
  SELECT pg_get_functiondef(item.oid) INTO source;original:=source;
  source:=replace(source,'invocation.resource_kind=''GITHUB_REPOSITORY''','invocation.resource_kind IN(''GITHUB_REPOSITORY'',''GITHUB_SCOPE'')');
  source:=replace(source,'(invocation.resource_scope->>''owner'')||''/''||(invocation.resource_scope->>''repository'')','control_plane.workflow_github_repository(invocation.resource_scope,invocation.bounded_input)');
  source:=replace(source,'(execution.resource_scope->>''owner'')||''/''||(execution.resource_scope->>''repository'')','control_plane.workflow_github_repository(execution.resource_scope,execution.bounded_input)');
  source:=replace(source,'execution.bounded_input-ARRAY','execution.bounded_input-(CASE WHEN execution.resource_scope ? ''scope_kind'' THEN ARRAY[''repository_id'',''repository_full_name''] ELSE ARRAY[]::text[] END)-ARRAY');
  source:=replace(source,'connection.enabled AND connection.state=''CONNECTED''','connection.enabled AND ((connection.credential_policy=''PROJECT_SHARED'' AND connection.state=''CONNECTED'') OR control_plane.workflow_personal_invocation_ready(invocation.id))');
  source:=replace(source,'AND invocation.approval_policy=''NONE''','AND ((invocation.resource_kind=''GITHUB_REPOSITORY'' AND invocation.approval_policy=''NONE'') OR (invocation.resource_kind=''GITHUB_SCOPE'' AND invocation.approval_policy IN(''HUMAN_SCOPED'',''HUMAN_EACH_EFFECT'')))');
  IF source=original THEN RAISE EXCEPTION 'GitHub workflow predecessor shape changed: %',item.oid::regprocedure;END IF;
  EXECUTE source;
 END LOOP;
 SELECT pg_get_functiondef('control_plane.workflow_pdlc_author_slot(jsonb,text,jsonb,jsonb)'::regprocedure) INTO source;
 needle:='BEGIN'||E'\n';replacement:='BEGIN'||E'\n'||$body$ IF p_scope ? 'scope_kind' THEN
  IF control_plane.workflow_github_repository(p_scope,p_input) IS NULL THEN RETURN NULL;END IF;
  p_scope:=jsonb_build_object('owner',split_part(p_input->>'repository_full_name','/',1),'repository',split_part(p_input->>'repository_full_name','/',2));
  p_input:=p_input-ARRAY['repository_id','repository_full_name'];
 END IF;
$body$;
 IF (length(source)-length(replace(source,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'GitHub author slot predecessor changed';END IF;
 EXECUTE replace(source,needle,replacement);
 SELECT pg_get_functiondef('control_plane.workflow_pdlc_file_desired_digest(text,text,jsonb)'::regprocedure) INTO source;
 needle:='BEGIN'||E'\n IF jsonb_typeof(p_input)';replacement:='BEGIN'||E'\n'||$body$ IF p_input ?| ARRAY['repository_id','repository_full_name'] THEN
  IF control_plane.workflow_github_repository('{"scope_kind":"ALL_ACCESSIBLE"}'::jsonb,p_input) IS NULL THEN RETURN NULL;END IF;
  p_scope_digest:=encode(sha256(convert_to('kodex.workflow-github-repository.v1','UTF8')||v_zero||convert_to(p_scope_digest,'UTF8')||v_zero||convert_to(p_input->>'repository_id','UTF8')||v_zero||convert_to(p_input->>'repository_full_name','UTF8')),'hex');
 END IF;
 IF jsonb_typeof(p_input)$body$;
 IF (length(source)-length(replace(source,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'GitHub file intent predecessor changed';END IF;
 EXECUTE replace(source,needle,replacement);
END $workflow_scope$;
-- +goose StatementEnd
SET LOCAL search_path=public;
RESET ROLE;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'GitHub repository scope migration is forward-only'; END $$;
-- +goose StatementEnd
