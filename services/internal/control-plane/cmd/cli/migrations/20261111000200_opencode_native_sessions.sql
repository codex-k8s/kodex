-- +goose Up
SET ROLE control_plane_owner;
SET LOCAL lock_timeout='5s';
LOCK TABLE control_plane.sessions,control_plane.session_storage,control_plane.session_archives IN ACCESS EXCLUSIVE MODE;

-- Переход без legacy decoder выполняется только после отдельной операторской
-- очистки старых сессий. Миграция сама не удаляет пользовательские данные.
-- +goose StatementBegin
DO $native_cutover$
BEGIN
 IF EXISTS (SELECT 1 FROM control_plane.sessions)
    OR EXISTS (SELECT 1 FROM control_plane.session_storage)
    OR EXISTS (SELECT 1 FROM control_plane.session_archives) THEN
   RAISE EXCEPTION 'OpenCode-only cutover requires an explicitly authorized session-data purge first';
 END IF;
END $native_cutover$;
-- +goose StatementEnd

-- Закрытая metadata-проекция повторяет native contract. Transcript хранится
-- в официальном export, а не в JSON-поле этой таблицы.
-- +goose StatementBegin
CREATE FUNCTION control_plane.native_checkpoint_shape(p_value jsonb) RETURNS boolean
LANGUAGE sql IMMUTABLE SET search_path=pg_catalog AS $$
 SELECT CASE WHEN jsonb_typeof(p_value)='object' THEN COALESCE(
   NOT EXISTS (SELECT 1 FROM jsonb_object_keys(p_value) key WHERE key NOT IN
     ('session_id','version','generation','relative_path','sha256','size_bytes','workspace_ref','workspace_generation'))
   AND jsonb_typeof(p_value->'session_id')='string' AND p_value->>'session_id' ~ '^ses_[A-Za-z0-9]{1,100}$'
   AND jsonb_typeof(p_value->'version')='string' AND p_value->>'version' ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
   AND jsonb_typeof(p_value->'generation')='number'
   AND (p_value->>'generation')::bigint BETWEEN 1 AND 9007199254740990
   AND jsonb_typeof(p_value->'relative_path')='string' AND p_value->>'relative_path'='.kodex/state/opencode/checkpoints/g'||(p_value->>'generation')||'/'||(p_value->>'session_id')||'.json'
   AND jsonb_typeof(p_value->'sha256')='string' AND p_value->>'sha256' ~ '^[a-f0-9]{64}$'
   AND jsonb_typeof(p_value->'size_bytes')='number'
   AND (p_value->>'size_bytes')::bigint BETWEEN 1 AND 67108864
   AND ((NOT p_value ? 'workspace_ref' AND NOT p_value ? 'workspace_generation')
     OR (jsonb_typeof(p_value->'workspace_ref')='string' AND p_value->>'workspace_ref' ~ '^wks_[A-Za-z0-9_-]{8,89}$'
       AND jsonb_typeof(p_value->'workspace_generation')='number'
       AND (p_value->>'workspace_generation')::bigint>0)),false)
 ELSE false END;
$$;
-- +goose StatementEnd

ALTER TABLE control_plane.session_storage RENAME COLUMN codex_session_id TO native_session_id;
ALTER TABLE control_plane.session_storage
 ALTER COLUMN native_session_id TYPE text USING native_session_id::text,
 ALTER COLUMN native_session_id DROP NOT NULL,
 ALTER COLUMN source_relative_path DROP NOT NULL,
 ALTER COLUMN source_sha256 DROP NOT NULL,
 ALTER COLUMN source_size_bytes DROP NOT NULL,
 DROP CONSTRAINT session_storage_source_relative_path_check,
 DROP CONSTRAINT session_storage_content_generation_check,
 DROP CONSTRAINT session_storage_state_check,
 ADD COLUMN native_checkpoint jsonb,
 ADD COLUMN native_writer_lease_ref text REFERENCES control_plane.runtime_leases(ref) DEFERRABLE INITIALLY IMMEDIATE,
 ADD COLUMN native_writer_revision_id uuid REFERENCES control_plane.runtime_revisions(id) DEFERRABLE INITIALLY IMMEDIATE,
 ADD CONSTRAINT session_storage_content_generation_check CHECK(content_generation>=0),
 ADD CONSTRAINT session_storage_state_check CHECK(state IN('INITIALIZING','LIVE','SNAPSHOT_READY','SNAPSHOTTING',
   'DELETE_PVC_READY','ARCHIVED','RESTORE_READY','RESTORING','ERROR','PURGED')),
 ADD CONSTRAINT session_storage_native_writer CHECK((native_writer_lease_ref IS NULL)=(native_writer_revision_id IS NULL)),
 ADD CONSTRAINT session_storage_native_writer_state CHECK(native_writer_lease_ref IS NULL OR state IN('INITIALIZING','LIVE','ERROR')),
 ADD CONSTRAINT session_storage_native_shape CHECK(
   (state='INITIALIZING' AND content_generation=0 AND native_session_id IS NULL AND native_checkpoint IS NULL
     AND source_relative_path IS NULL AND source_sha256 IS NULL AND source_size_bytes IS NULL
     AND native_writer_lease_ref IS NOT NULL AND current_archive_id IS NULL)
   OR (content_generation>0 AND state<>'INITIALIZING' AND native_session_id IS NOT NULL
     AND source_relative_path IS NOT NULL AND source_sha256 IS NOT NULL AND source_size_bytes IS NOT NULL
     AND native_checkpoint IS NOT NULL AND control_plane.native_checkpoint_shape(native_checkpoint)
     AND native_checkpoint->>'session_id'=native_session_id
     AND (native_checkpoint->>'generation')::bigint=content_generation
     AND native_checkpoint->>'relative_path'=source_relative_path
     AND native_checkpoint->>'sha256'=source_sha256
     AND (native_checkpoint->>'size_bytes')::bigint=source_size_bytes));
CREATE UNIQUE INDEX session_storage_native_identity ON control_plane.session_storage(native_session_id)
 WHERE native_session_id IS NOT NULL;

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_native_session_identity() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF OLD.native_session_id IS NOT NULL AND NEW.native_session_id IS DISTINCT FROM OLD.native_session_id THEN
   RAISE EXCEPTION 'native session identity is immutable; create an explicit new session';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER guard_native_session_identity BEFORE UPDATE ON control_plane.session_storage
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_native_session_identity();

ALTER TABLE control_plane.session_archives RENAME COLUMN codex_session_id TO native_session_id;
ALTER TABLE control_plane.session_archives
 ALTER COLUMN native_session_id TYPE text USING native_session_id::text,
 DROP CONSTRAINT session_archives_source_relative_path_check,
 DROP CONSTRAINT session_archives_format_shape,
 ADD COLUMN native_checkpoint jsonb,
 ADD CONSTRAINT session_archives_format_shape CHECK(
   (format_version=3 AND native_session_id IS NOT NULL AND source_relative_path IS NOT NULL
     AND source_size_bytes BETWEEN 1 AND 67108864 AND workspace_id IS NULL AND workspace_generation IS NULL
     AND workspace_checkpoint_ref IS NULL AND workspace_entries IS NULL AND workspace_quiescence IS NULL
     AND native_checkpoint IS NOT NULL AND control_plane.native_checkpoint_shape(native_checkpoint)
     AND native_checkpoint->>'session_id'=native_session_id
     AND (native_checkpoint->>'generation')::bigint=content_generation
     AND native_checkpoint->>'relative_path'=source_relative_path
     AND native_checkpoint->>'sha256'=source_sha256
     AND (native_checkpoint->>'size_bytes')::bigint=source_size_bytes)
   OR (format_version=2 AND native_session_id IS NULL AND native_checkpoint IS NULL AND source_relative_path IS NULL AND project_id IS NOT NULL
     AND source_size_bytes BETWEEN 0 AND 1073741824 AND workspace_id IS NOT NULL AND workspace_generation IS NOT NULL AND workspace_generation>0
     AND workspace_checkpoint_ref IS NOT NULL AND workspace_checkpoint_ref ~ '^wcp_[A-Za-z0-9_-]{8,89}$'
     AND workspace_entries IS NOT NULL AND workspace_entries BETWEEN 0 AND 100000
     AND (source_size_bytes=0 OR workspace_entries>0) AND workspace_quiescence IS NOT NULL AND octet_length(workspace_quiescence) BETWEEN 1 AND 2048
     AND object_size_bytes<=65536 AND char_length(object_version)>0));

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.protect_session_archive_receipt()
RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'session archive receipt is immutable'; END IF;
 IF ROW(NEW.id,NEW.ref,NEW.organization_id,NEW.session_id,NEW.provider_account_id,
        NEW.runtime_revision_id,NEW.native_session_id,NEW.native_checkpoint,NEW.content_generation,
        NEW.format_version,NEW.source_relative_path,NEW.source_sha256,NEW.source_size_bytes,
        NEW.object_key,NEW.object_version,NEW.object_etag,NEW.object_digest,NEW.object_size_bytes,NEW.created_at)
    IS DISTINCT FROM
    ROW(OLD.id,OLD.ref,OLD.organization_id,OLD.session_id,OLD.provider_account_id,
        OLD.runtime_revision_id,OLD.native_session_id,OLD.native_checkpoint,OLD.content_generation,
        OLD.format_version,OLD.source_relative_path,OLD.source_sha256,OLD.source_size_bytes,
        OLD.object_key,OLD.object_version,OLD.object_etag,OLD.object_digest,OLD.object_size_bytes,OLD.created_at)
    OR (NEW.project_id IS DISTINCT FROM OLD.project_id AND NOT (
      OLD.project_id IS NULL AND NEW.project_id IS NOT NULL AND EXISTS (
        SELECT 1 FROM control_plane.sessions session WHERE session.id=NEW.session_id
         AND session.organization_id=NEW.organization_id AND session.project_id=NEW.project_id))) THEN
   RAISE EXCEPTION 'session archive receipt identity is immutable';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION control_plane.native_checkpoint_shape(jsonb),control_plane.guard_native_session_identity() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.native_checkpoint_shape(jsonb),control_plane.guard_native_session_identity() TO control_plane_runtime;

-- Новые связи writer входят в полный purge-граф. Сначала доказываются прежний
-- граф без ровно этих двух рёбер, их точная форма и неизменность owner-функции.
-- Остальные связи не исключаются, grants и алгоритм удаления не меняются.
-- +goose StatementBegin
DO $native_purge$
DECLARE
 v_function oid:='control_plane.purge_project_database(uuid,uuid,text)'::regprocedure;
 v_definition text; v_source text; v_nodes oid[]; v_count integer; v_digest text;
 v_old_nodes text:='IF v_nodes <> 111 OR v_node_fingerprint <> ''3eb558fae167872ba6bd17bc68f7a303'' THEN';
 v_old_edges text:='IF v_edges <> 306 OR v_edge_fingerprint <> ''19782286a860f9e05cdaad7b6e5f8ef3'' THEN';
 v_new_edges text; v_delta oid[]; v_links jsonb;
BEGIN
 SELECT pg_get_functiondef(p.oid),p.prosrc INTO STRICT v_definition,v_source
 FROM pg_proc p WHERE p.oid=v_function AND p.proowner='control_plane_owner'::regrole
  AND p.prosecdef AND p.provolatile='v' AND p.prorettype='integer'::regtype
  AND NOT p.proretset AND p.proconfig=ARRAY['search_path=pg_catalog']
  AND has_function_privilege('control_plane_runtime',p.oid,'EXECUTE')
  AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl
   WHERE acl.grantee NOT IN('control_plane_owner'::regrole,'control_plane_runtime'::regrole)
    OR acl.grantee='control_plane_runtime'::regrole AND (acl.is_grantable OR acl.privilege_type<>'EXECUTE'));
 IF regexp_count(v_source,'IF v_nodes <>')<>1 OR regexp_count(v_source,'IF v_edges <>')<>1
  OR md5(replace(replace(v_source,v_old_nodes,
    'IF v_nodes <> 105 OR v_node_fingerprint <> ''f51a0de25bd942a250997775617eeaee'' THEN'),v_old_edges,
    'IF v_edges <> 284 OR v_edge_fingerprint <> ''6bf6ad76d8c7ae66e07f75ecf719fdd6'' THEN'))<>'b886c24d4e2f5c4a0ec08123d99462b0' THEN
  RAISE EXCEPTION 'native session purge predecessor changed';
 END IF;
 SELECT array_agg(c.oid),jsonb_agg(jsonb_build_array(c.conname,pg_get_constraintdef(c.oid),
   c.convalidated,c.conenforced) ORDER BY c.conname) INTO v_delta,v_links
 FROM pg_constraint c WHERE c.conrelid='control_plane.session_storage'::regclass
  AND c.conname IN('session_storage_native_writer_lease_ref_fkey','session_storage_native_writer_revision_id_fkey');
 IF v_links IS DISTINCT FROM '[
  ["session_storage_native_writer_lease_ref_fkey","FOREIGN KEY (native_writer_lease_ref) REFERENCES control_plane.runtime_leases(ref) DEFERRABLE",true,true],
  ["session_storage_native_writer_revision_id_fkey","FOREIGN KEY (native_writer_revision_id) REFERENCES control_plane.runtime_revisions(id) DEFERRABLE",true,true]]'::jsonb THEN
  RAISE EXCEPTION 'native session purge graph delta changed';
 END IF;
 WITH RECURSIVE nodes(id) AS (SELECT 'control_plane.projects'::regclass::oid UNION
  SELECT c.conrelid FROM pg_constraint c JOIN nodes n ON c.confrelid=n.id
  WHERE c.contype='f' AND c.connamespace='control_plane'::regnamespace)
 SELECT array_agg(id) INTO v_nodes FROM nodes;
 SELECT count(*),md5(string_agg(n.nspname||'.'||r.relname,E'\n' ORDER BY n.nspname,r.relname))
 INTO v_count,v_digest FROM pg_class r JOIN pg_namespace n ON n.oid=r.relnamespace WHERE r.oid=ANY(v_nodes);
 IF v_count<>111 OR v_digest<>'3eb558fae167872ba6bd17bc68f7a303' THEN
  RAISE EXCEPTION 'native session purge nodes changed';
 END IF;
 SELECT count(*),md5(string_agg(cn.nspname||'.'||cc.relname||'|'||c.conname||'|'||pn.nspname||'.'||pc.relname||'|'||c.confdeltype::text,
  E'\n' ORDER BY cn.nspname,cc.relname,c.conname)) INTO v_count,v_digest
 FROM pg_constraint c JOIN pg_class cc ON cc.oid=c.conrelid JOIN pg_namespace cn ON cn.oid=cc.relnamespace
 JOIN pg_class pc ON pc.oid=c.confrelid JOIN pg_namespace pn ON pn.oid=pc.relnamespace
 WHERE c.contype='f' AND c.conrelid=ANY(v_nodes) AND c.confrelid=ANY(v_nodes) AND NOT c.oid=ANY(v_delta);
 IF v_count<>306 OR v_digest<>'19782286a860f9e05cdaad7b6e5f8ef3' THEN
  RAISE EXCEPTION 'native session purge predecessor edges changed';
 END IF;
 SELECT count(*),md5(string_agg(cn.nspname||'.'||cc.relname||'|'||c.conname||'|'||pn.nspname||'.'||pc.relname||'|'||c.confdeltype::text,
  E'\n' ORDER BY cn.nspname,cc.relname,c.conname)) INTO v_count,v_digest
 FROM pg_constraint c JOIN pg_class cc ON cc.oid=c.conrelid JOIN pg_namespace cn ON cn.oid=cc.relnamespace
 JOIN pg_class pc ON pc.oid=c.confrelid JOIN pg_namespace pn ON pn.oid=pc.relnamespace
 WHERE c.contype='f' AND c.conrelid=ANY(v_nodes) AND c.confrelid=ANY(v_nodes);
 IF v_count<>308 OR v_digest<>'d6aa5b9aa21c5e065c3106f2a6119e0f' THEN
  RAISE EXCEPTION 'native session purge successor edges changed';
 END IF;
 v_new_edges:=format('IF v_edges <> %s OR v_edge_fingerprint <> %L THEN',v_count,v_digest);
 EXECUTE replace(v_definition,v_old_edges,v_new_edges);
 IF NOT EXISTS(SELECT 1 FROM pg_proc p WHERE p.oid=v_function AND p.prosrc=replace(v_source,v_old_edges,v_new_edges)) THEN
  RAISE EXCEPTION 'native session purge function readback failed';
 END IF;
END $native_purge$;
-- +goose StatementEnd

-- Новый runtime сохраняет исторические immutable file pins и точный SERVER_DAG
-- frontier, потерянные при замене функции миграцией optional antivirus.
-- Допуск NOT_SCANNED обычных файлов сохраняется; CLEAN для skills и workflow
-- evidence остаётся обязательным в их специализированных owner-функциях.
-- +goose StatementBegin
DO $native_file_boundary$
BEGIN
 IF (SELECT md5(prosrc) FROM pg_proc
     WHERE oid='control_plane.runtime_file_source_visible(uuid,uuid,uuid,uuid,uuid,text,text,uuid)'::regprocedure)
     IS DISTINCT FROM '9a14fe7cf18830648738eec9b80aa709' THEN
   RAISE EXCEPTION 'native runtime file visibility predecessor changed';
 END IF;
END $native_file_boundary$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.runtime_file_source_visible(
    p_tenant uuid,p_actor uuid,p_project uuid,p_agent uuid,p_artifact uuid,p_purpose text,p_source_revision text,p_node uuid
) RETURNS boolean LANGUAGE sql STABLE SECURITY INVOKER
SET search_path=pg_catalog,control_plane AS $$
    SELECT EXISTS (
        SELECT 1 FROM control_plane.catalog_access_targets file_target
        JOIN control_plane.artifact_history file ON file.id=file_target.id AND file.organization_id=p_tenant
        JOIN control_plane.artifact_revision_content content ON content.revision_id=file.revision_id
          AND content.digest=file.digest AND content.size_bytes=file.size_bytes
        JOIN control_plane.catalog_access_targets project_target
          ON project_target.organization_id=p_tenant AND project_target.kind='PROJECT' AND project_target.id=p_project
        LEFT JOIN control_plane.catalog_access_targets agent_target
          ON agent_target.organization_id=p_tenant AND agent_target.kind='AGENT' AND agent_target.id=p_agent
          AND agent_target.project_id=p_project
        JOIN control_plane.agents current_agent ON current_agent.id=p_agent AND current_agent.organization_id=p_tenant
        WHERE file_target.organization_id=p_tenant AND file_target.kind='ARTIFACT' AND file.id=p_artifact
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
              AND CASE WHEN EXISTS (
                SELECT 1 FROM control_plane.run_nodes execution
                JOIN control_plane.runs root ON root.id=execution.root_run_id AND root.organization_id=execution.organization_id
                JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=p_tenant
                WHERE execution.id=p_node AND execution.organization_id=p_tenant AND version.spec->>'ExecutionMode'='SERVER_DAG_V1'
              ) THEN EXISTS (
                SELECT 1 FROM control_plane.run_nodes execution
                JOIN control_plane.runs root ON root.id=execution.root_run_id AND root.organization_id=p_tenant
                WHERE execution.id=p_node AND execution.organization_id=p_tenant AND execution.agent_id=p_agent
                  AND root.project_id=p_project AND root.initiated_by=p_actor
                  AND control_plane.runtime_workflow_input_visible(p_tenant,p_node,file.revision_id)
              ) ELSE (
                (control_plane.runtime_file_coordinator(p_tenant,p_actor,p_project,p_agent,p_node)
                 AND EXISTS (SELECT 1 FROM control_plane.runtime_received_result_artifacts(p_tenant,p_node) pin
                     WHERE pin->>'ref'=file.ref AND pin->>'digest'=file.digest
                       AND pin->'revision'=to_jsonb(file.revision)
                       AND pin->'sizeBytes'=to_jsonb(file.size_bytes) AND pin->>'fileName'=file.file_name
                       AND pin->>'mediaType'=file.media_type AND pin->>'source'=file.source))
                OR (NOT control_plane.runtime_file_coordinator(p_tenant,p_actor,p_project,p_agent,p_node)
                    AND 'platform.artifact.manage'=ANY(current_agent.capabilities)
                    AND NOT EXISTS (SELECT 1 FROM control_plane.run_nodes execution
                        WHERE execution.id=p_node AND execution.workflow_step_key LIKE 'workflow.coordinator.%'))
              ) END
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

RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $native_forward_only$
BEGIN
 RAISE EXCEPTION 'OpenCode-only session cutover is forward-only';
END $native_forward_only$;
-- +goose StatementEnd
