-- +goose Up
SET ROLE control_plane_owner;
SET LOCAL lock_timeout = '8s';
SET LOCAL statement_timeout = '30s';

-- Проверяется ровно изменяемый guard; старые миграции и данные не переписываются.
-- +goose StatementBegin
DO $predecessor$
BEGIN
    IF (SELECT max(version_id) FROM public.goose_db_version WHERE is_applied) IS DISTINCT FROM 20261110000200::bigint OR
        NOT EXISTS (SELECT 1 FROM pg_catalog.pg_proc
            WHERE oid='control_plane.guard_session_workspace_storage()'::regprocedure
                AND md5(prosrc)='e4aed9f09b46c006f08aecea011ee8c6') OR
        NOT EXISTS (SELECT 1 FROM pg_catalog.pg_proc
            WHERE oid='control_plane.guard_workspace_archive_task()'::regprocedure
                AND md5(prosrc)='93a6276029ab000b40c090d846748cad') THEN
        RAISE EXCEPTION 'workflow workspace requires the unchanged managed workspace predecessor';
    END IF;
END $predecessor$;
-- +goose StatementEnd

ALTER TABLE control_plane.session_workspace_storage
    ADD COLUMN workflow_version_id uuid;
-- Версия уже удерживается FK корневого run. Guard ниже доказывает exact root/version;
-- дополнительное ребро purge-графа для того же immutable pin не требуется.

-- Полномочия не следуют из этой функции: она сверяет только immutable scope.
-- Активность, grants, initiator, claim и writer fence проверяет прежний owner lifecycle.
-- +goose StatementBegin
CREATE FUNCTION control_plane.workspace_session_scope_matches(p_session_id uuid,p_agent_id uuid,p_workflow_version_id uuid)
RETURNS boolean LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog AS $$
    SELECT EXISTS (
        SELECT 1 FROM control_plane.sessions session
        JOIN control_plane.agents agent ON agent.id=p_agent_id
            AND agent.organization_id=session.organization_id AND agent.project_id=session.project_id
        WHERE session.id=p_session_id AND (
            (session.target_type='AGENT' AND session.target_ref=agent.ref AND p_workflow_version_id IS NULL)
            OR (session.target_type='WORKFLOW' AND EXISTS (
                SELECT 1 FROM control_plane.workflow_versions version
                JOIN control_plane.workflows workflow ON workflow.id=version.workflow_id
                    AND workflow.organization_id=session.organization_id AND workflow.project_id=session.project_id
                    AND workflow.ref=session.target_ref
                JOIN control_plane.runs root ON root.workflow_version_id=version.id AND root.root_run_id=root.id
                    AND root.organization_id=session.organization_id AND root.project_id=session.project_id
                    AND root.session_id=session.id AND root.target_type='WORKFLOW' AND root.target_ref=workflow.ref
                WHERE version.id=p_workflow_version_id AND version.organization_id=session.organization_id
                    AND version.spec->>'CoordinatorAgentRef'=agent.ref
            ))
        )
    );
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.workspace_session_scope_matches(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.workspace_session_scope_matches(uuid,uuid,uuid) TO control_plane_runtime;

-- Полное определение guard сохраняет проверяемые переходы и writer custody.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.guard_session_workspace_storage() RETURNS trigger
LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog AS $$
DECLARE
    v_ack jsonb;
    v_join jsonb;
BEGIN
    IF TG_OP='DELETE' THEN
        IF NOT control_plane.project_purge_authorized() OR OLD.state<>'PURGED' THEN
            RAISE EXCEPTION 'session workspace metadata is retained';
        END IF;
        RETURN OLD;
    END IF;
    IF NOT control_plane.workspace_session_scope_matches(NEW.session_id,NEW.agent_id,NEW.workflow_version_id) OR
        NOT EXISTS (SELECT 1 FROM control_plane.sessions session
            WHERE session.id=NEW.session_id AND session.organization_id=NEW.organization_id
                AND session.project_id=NEW.project_id) THEN
        RAISE EXCEPTION 'session workspace scope is invalid';
    END IF;
    IF TG_OP='INSERT' THEN
        IF NEW.generation<>1 OR NEW.state<>'EMPTY' OR NEW.version<>1 OR NEW.writer_lease_id IS NOT NULL OR
            NEW.source_archive_id IS NOT NULL OR NEW.current_archive_id IS NOT NULL OR NEW.writer_released THEN
            RAISE EXCEPTION 'session workspace initial state is invalid';
        END IF;
    ELSE
        IF ROW(NEW.id,NEW.ref,NEW.organization_id,NEW.project_id,NEW.session_id,NEW.agent_id,NEW.workflow_version_id,NEW.created_at)
            IS DISTINCT FROM ROW(OLD.id,OLD.ref,OLD.organization_id,OLD.project_id,OLD.session_id,OLD.agent_id,OLD.workflow_version_id,OLD.created_at)
            OR NEW.version<>OLD.version+1 OR OLD.state='PURGED' THEN
            RAISE EXCEPTION 'session workspace identity is immutable';
        END IF;
        IF NEW.generation<>OLD.generation THEN
            IF NEW.generation<>OLD.generation+1 OR OLD.state NOT IN ('CHECKPOINT_READY','RESTORED') OR
                NEW.state<>'RESTORE_READY' OR NEW.source_archive_id IS NULL OR
                NEW.source_archive_id IS DISTINCT FROM OLD.current_archive_id OR NEW.writer_lease_id IS NOT NULL OR
                NOT OLD.writer_released OR NEW.writer_released OR
                NEW.writer_acknowledgement IS NOT NULL OR NEW.quiescence_record IS NOT NULL OR NEW.current_archive_id IS NOT NULL THEN
                RAISE EXCEPTION 'session workspace generation is not checkpointed';
            END IF;
        ELSIF NEW.state<>OLD.state AND NOT (
            (OLD.state IN ('EMPTY','RESTORED') AND NEW.state='WRITER_PENDING') OR
            (OLD.state='WRITER_PENDING' AND NEW.state='WRITING') OR
            (OLD.state='WRITING' AND NEW.state IN ('QUIESCING','CHECKPOINT_PENDING')) OR
            (OLD.state='QUIESCING' AND NEW.state='CHECKPOINT_PENDING') OR
            (OLD.state='CHECKPOINT_PENDING' AND NEW.state='CHECKPOINT_READY' AND OLD.writer_released) OR
            (OLD.state='CHECKPOINT_READY' AND NEW.state='DELETE_READY') OR
            (OLD.state='DELETE_READY' AND NEW.state='ARCHIVED') OR
            (OLD.state='ARCHIVED' AND NEW.state='RESTORE_READY') OR
            (OLD.state='RESTORE_READY' AND NEW.state='RESTORING') OR
            (OLD.state='RESTORING' AND NEW.state='RESTORED') OR
            (OLD.state='BLOCKED' AND NEW.state='CHECKPOINT_PENDING' AND OLD.writer_acknowledgement IS NOT NULL) OR
            (NEW.state='BLOCKED' AND NEW.quiescence_record IS NULL) OR
            (NEW.state='PURGED' AND OLD.state IN ('CHECKPOINT_READY','ARCHIVED','RESTORED'))
        ) THEN
            RAISE EXCEPTION 'session workspace transition is invalid';
        END IF;
        IF NEW.generation=OLD.generation AND (
            (OLD.writer_acknowledgement IS NOT NULL AND NEW.writer_acknowledgement IS DISTINCT FROM OLD.writer_acknowledgement) OR
            (OLD.quiescence_record IS NOT NULL AND NEW.quiescence_record IS DISTINCT FROM OLD.quiescence_record) OR
            (OLD.writer_lease_id IS NOT NULL AND NEW.writer_lease_id IS DISTINCT FROM OLD.writer_lease_id) OR
            (NEW.source_archive_id IS DISTINCT FROM OLD.source_archive_id) OR
            (OLD.current_archive_id IS NOT NULL AND NEW.current_archive_id IS DISTINCT FROM OLD.current_archive_id)
        ) THEN
            RAISE EXCEPTION 'session workspace execution pins are immutable';
        END IF;
        IF NEW.generation=OLD.generation AND (
            (OLD.writer_released AND NOT NEW.writer_released) OR
            (NOT OLD.writer_released AND NEW.writer_released AND
                (OLD.state<>'CHECKPOINT_PENDING' OR NEW.state<>'CHECKPOINT_PENDING' OR OLD.quiescence_record IS NULL))
        ) THEN
            RAISE EXCEPTION 'session workspace released writer is immutable';
        END IF;
        IF OLD.block_reason<>'' AND NEW.block_reason IS DISTINCT FROM OLD.block_reason AND
            NOT (NEW.state='CHECKPOINT_PENDING' AND NEW.block_reason='' AND NEW.quiescence_record IS NOT NULL) THEN
            RAISE EXCEPTION 'session workspace blocked cause is immutable';
        END IF;
    END IF;
    IF NEW.writer_lease_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM control_plane.runtime_leases lease
        JOIN control_plane.runtime_revisions revision ON revision.id=lease.runtime_revision_id
            AND revision.organization_id=NEW.organization_id AND revision.session_id=NEW.session_id
            AND revision.project_id=NEW.project_id AND revision.agent_id=NEW.agent_id
        JOIN control_plane.runs writer_run ON writer_run.id=revision.run_id
            AND writer_run.organization_id=NEW.organization_id AND writer_run.project_id=NEW.project_id
            AND writer_run.session_id=NEW.session_id
            AND (NEW.workflow_version_id IS NULL OR writer_run.workflow_version_id=NEW.workflow_version_id)
        WHERE lease.id=NEW.writer_lease_id AND lease.organization_id=NEW.organization_id
    ) THEN
        RAISE EXCEPTION 'session workspace writer scope is invalid';
    END IF;
    IF NEW.writer_acknowledgement IS NOT NULL THEN
        v_ack:=NEW.writer_acknowledgement::jsonb;
        IF jsonb_typeof(v_ack)<>'object' OR (SELECT count(*) FROM jsonb_object_keys(v_ack))<>5 OR
            NOT (v_ack ?& ARRAY['schema','scope','workspace_ref','generation','writer']) OR
            v_ack->>'schema' IS DISTINCT FROM 'kodex.runtime-workspace-writer.v1' OR v_ack->>'workspace_ref' IS DISTINCT FROM NEW.ref OR
            v_ack->'generation' IS DISTINCT FROM to_jsonb(NEW.generation) OR jsonb_typeof(v_ack->'writer') IS DISTINCT FROM 'object' OR
            (SELECT count(*) FROM jsonb_object_keys(v_ack->'writer'))<>14 OR
            NOT ((v_ack->'writer') ?& ARRAY['run_ref','node_ref','turn_ref','runtime_revision_ref','runtime_revision_version',
                'runtime_revision_digest','execution_binding_digest','input_digest','lease_ref','lease_generation','attempt','pod_uid','pvc_uid','join_sha256']) OR
            v_ack->'writer'->>'join_sha256' IS DISTINCT FROM '' OR
            ((v_ack->'writer'->>'pod_uid') ~ '^[a-z0-9][a-z0-9-]{0,127}$') IS NOT TRUE OR
            ((v_ack->'writer'->>'pvc_uid') ~ '^[a-z0-9][a-z0-9-]{0,127}$') IS NOT TRUE OR
            ((v_ack->'writer'->>'execution_binding_digest') ~ '^[a-f0-9]{64}$') IS NOT TRUE OR
            NOT EXISTS (SELECT 1 FROM control_plane.runtime_leases lease
                JOIN control_plane.runtime_revisions revision ON revision.id=lease.runtime_revision_id
                JOIN control_plane.sessions session ON session.id=NEW.session_id
                JOIN control_plane.runs run ON run.id=lease.run_id AND run.session_id=NEW.session_id
                JOIN control_plane.run_nodes node ON node.id=lease.node_id AND node.agent_id=NEW.agent_id AND node.run_id=run.id
                JOIN control_plane.session_turns turn ON turn.id=revision.turn_id AND turn.session_id=NEW.session_id AND turn.run_id=run.id
                JOIN control_plane.agents agent ON agent.id=NEW.agent_id
                JOIN control_plane.projects project ON project.id=NEW.project_id
                JOIN control_plane.organizations organization ON organization.id=NEW.organization_id
                WHERE lease.id=NEW.writer_lease_id AND v_ack->'writer'->>'lease_ref'=lease.ref
                    AND v_ack->'writer'->'lease_generation'=to_jsonb(lease.generation)
                    AND v_ack->'writer'->>'runtime_revision_ref'=revision.ref
                    AND v_ack->'writer'->'runtime_revision_version'=to_jsonb(revision.generation)
                    AND v_ack->'writer'->>'runtime_revision_digest'=revision.revision_digest
                    AND v_ack->'writer'->>'input_digest'=lease.input_digest
                    AND v_ack->'writer'->>'run_ref'=run.ref AND v_ack->'writer'->>'node_ref'=node.ref
                    AND v_ack->'writer'->>'turn_ref'=turn.ref AND v_ack->'writer'->'attempt'=to_jsonb(revision.attempt)
                    AND v_ack->'scope'=jsonb_build_object('organization_ref',organization.ref,'project_ref',project.ref,'session_ref',session.ref,'agent_ref',agent.ref)) THEN
            RAISE EXCEPTION 'session workspace writer acknowledgement is invalid';
        END IF;
    END IF;
    IF NEW.quiescence_record IS NOT NULL THEN
        v_join:=NEW.quiescence_record::jsonb;
        IF v_ack IS NULL OR jsonb_typeof(v_join)<>'object' OR (SELECT count(*) FROM jsonb_object_keys(v_join))<>6 OR
            NOT (v_join ?& ARRAY['schema','scope','workspace_ref','generation','writer','joined']) OR
            v_join->>'schema' IS DISTINCT FROM 'kodex.runtime-workspace-quiescence.v1' OR v_join->'joined' IS DISTINCT FROM 'true'::jsonb OR
            (v_join-'schema'-'joined') IS DISTINCT FROM (v_ack-'schema') THEN
            RAISE EXCEPTION 'session workspace quiescence is invalid';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- Snapshot и restore используют тот же exact scope; custody переходы сохраняются.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.guard_workspace_archive_task() RETURNS trigger
LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog AS $$
DECLARE v_expected jsonb; v_checkpoint text;
BEGIN
 IF TG_OP='DELETE' THEN
   IF OLD.workspace_id IS NOT NULL AND (NOT control_plane.project_purge_authorized() OR OLD.state IN ('READY','CLAIMED')) THEN
     RAISE EXCEPTION 'workspace archive task custody is retained';
   END IF;
   RETURN OLD;
 END IF;
 IF TG_OP='UPDATE' THEN
   IF ROW(NEW.workspace_id,NEW.workspace_generation,NEW.workspace_input) IS DISTINCT FROM ROW(OLD.workspace_id,OLD.workspace_generation,OLD.workspace_input) THEN
     RAISE EXCEPTION 'workspace archive task profile is immutable';
   END IF;
   IF NEW.workspace_id IS NULL THEN RETURN NEW; END IF;
   IF ROW(NEW.id,NEW.ref,NEW.organization_id,NEW.project_id,NEW.session_id,NEW.archive_id,NEW.kind,NEW.content_generation,NEW.object_key,NEW.object_version,NEW.maximum_attempts,NEW.created_at)
      IS DISTINCT FROM ROW(OLD.id,OLD.ref,OLD.organization_id,OLD.project_id,OLD.session_id,OLD.archive_id,OLD.kind,OLD.content_generation,OLD.object_key,OLD.object_version,OLD.maximum_attempts,OLD.created_at) THEN
     RAISE EXCEPTION 'workspace archive task identity is immutable';
   END IF;
   IF OLD.state IN ('SUCCEEDED','CANCELLED','DEAD_LETTER') AND
     ROW(NEW.state,NEW.attempt,NEW.generation,NEW.workload_instance,NEW.lease_ref,NEW.fence_digest,NEW.lease_expires_at,NEW.input_digest,NEW.safe_error_code,NEW.completed_at,NEW.available_at)
     IS DISTINCT FROM ROW(OLD.state,OLD.attempt,OLD.generation,OLD.workload_instance,OLD.lease_ref,OLD.fence_digest,OLD.lease_expires_at,OLD.input_digest,OLD.safe_error_code,OLD.completed_at,OLD.available_at) THEN
     RAISE EXCEPTION 'workspace archive terminal task is immutable';
   END IF;
   IF NEW.state='CLAIMED' THEN
     IF NEW.generation=OLD.generation THEN
       IF OLD.state<>'CLAIMED' OR ROW(NEW.attempt,NEW.workload_instance,NEW.lease_ref,NEW.fence_digest,NEW.input_digest)
          IS DISTINCT FROM ROW(OLD.attempt,OLD.workload_instance,OLD.lease_ref,OLD.fence_digest,OLD.input_digest)
          OR NEW.lease_expires_at<OLD.lease_expires_at THEN
         RAISE EXCEPTION 'workspace archive renewal changed its grant'; END IF;
     ELSIF NEW.generation<>OLD.generation+1 OR NEW.attempt<>OLD.attempt+1 OR
       NOT(OLD.state='READY' AND OLD.available_at<=clock_timestamp() OR OLD.state='CLAIMED' AND OLD.lease_expires_at<=clock_timestamp()) THEN
       RAISE EXCEPTION 'workspace archive claim generation is invalid';
     END IF;
   ELSIF ROW(NEW.attempt,NEW.generation,NEW.input_digest) IS DISTINCT FROM ROW(OLD.attempt,OLD.generation,OLD.input_digest) OR
     NEW.state<>OLD.state AND NOT(OLD.state='CLAIMED' AND NEW.state IN ('READY','SUCCEEDED','CANCELLED','DEAD_LETTER') OR OLD.state='READY' AND NEW.state='CANCELLED') THEN
     RAISE EXCEPTION 'workspace archive task transition is invalid';
   END IF;
   RETURN NEW;
 END IF;
 IF NEW.workspace_id IS NULL THEN RETURN NEW; END IF;
 IF NEW.kind='RESTORE' THEN
   IF NEW.state<>'READY' OR NEW.attempt<>0 OR NEW.generation<>0 OR NEW.archive_id IS NULL OR NEW.maximum_attempts<>5 THEN
     RAISE EXCEPTION 'workspace restore task initial state is invalid';
   END IF;
   SELECT jsonb_build_object(
     'target',jsonb_build_object('profile','SESSION_WORKSPACE_V2',
       'scope',jsonb_build_object('organization_ref',o.ref,'project_ref',p.ref,'session_ref',s.ref,'agent_ref',a.ref),
       'workspace_ref',w.ref,'generation',w.generation,'volume_ready',v.ready_record::jsonb),
     'checkpoint_ref',source.workspace_checkpoint_ref,'pvc_uid',v.pvc_uid,'reconcile_only',false,
     'policy',jsonb_build_object('revision',r.safe_snapshot #> '{workspacePolicy,revision}',
       'root',r.safe_snapshot #> '{workspacePolicy,root}','rules',r.safe_snapshot #> '{workspacePolicy,rules}',
       'maximum_writable_bytes',r.safe_snapshot #> '{workspacePolicy,maximumWritableBytes}',
       'maximum_file_count',r.safe_snapshot #> '{workspacePolicy,maximumFileCount}',
       'digest',r.safe_snapshot #> '{workspacePolicy,digest}','denial_reasons',r.safe_snapshot #> '{workspacePolicy,denialReasons}'),
     'checkpoint',jsonb_build_object(
       'scope',jsonb_build_object('organization_ref',o.ref,'project_ref',p.ref,'session_ref',s.ref,'agent_ref',a.ref),
       'workspace_ref',w.ref,'generation',source.workspace_generation,'checkpoint_ref',source.workspace_checkpoint_ref,
       'tree_sha256',source.source_sha256,'bytes',source.source_size_bytes,'entries',source.workspace_entries,
       'manifest',jsonb_build_object('key',source.object_key,'version',source.object_version,'etag',source.object_etag,
         'digest',source.object_digest,'size_bytes',source.object_size_bytes))) INTO v_expected
   FROM control_plane.session_workspace_storage w
   JOIN control_plane.sessions s ON s.id=w.session_id AND s.organization_id=w.organization_id AND s.project_id=w.project_id
   JOIN control_plane.agents a ON a.id=w.agent_id AND a.organization_id=w.organization_id AND a.project_id=w.project_id AND control_plane.workspace_session_scope_matches(s.id,a.id,w.workflow_version_id)
   JOIN control_plane.projects p ON p.id=w.project_id AND p.organization_id=w.organization_id
   JOIN control_plane.organizations o ON o.id=w.organization_id
   JOIN control_plane.session_archives source ON source.id=w.source_archive_id AND source.workspace_id=w.id
     AND source.organization_id=w.organization_id AND source.project_id=w.project_id AND source.session_id=w.session_id
   JOIN control_plane.runtime_revisions r ON r.id=source.runtime_revision_id AND r.organization_id=w.organization_id
     AND r.project_id=w.project_id AND r.session_id=w.session_id AND r.agent_id=w.agent_id
   JOIN control_plane.session_workspace_volumes v ON v.workspace_id=w.id
   WHERE w.id=NEW.workspace_id AND w.organization_id=NEW.organization_id AND w.project_id=NEW.project_id
     AND w.session_id=NEW.session_id AND w.generation=NEW.workspace_generation
     AND w.state='RESTORE_READY' AND w.writer_lease_id IS NULL AND NOT w.writer_released
     AND w.writer_acknowledgement IS NULL AND w.quiescence_record IS NULL AND w.destructive_task_id IS NULL
     AND source.id=NEW.archive_id AND source.format_version=2 AND source.lifecycle_state='AVAILABLE'
     AND source.deleted_at IS NULL AND source.workspace_generation=w.generation-1
     AND v.state='READY' AND v.pvc_uid IS NOT NULL AND v.ready_record IS NOT NULL AND NOT v.physical_deleted
     AND p.lifecycle IN ('ACTIVE','TRASHED') AND r.safe_snapshot->'roleRuntimeContractRevision'='4'::jsonb
     AND NEW.object_key IS NULL AND NEW.object_version=source.object_version;
   IF v_expected IS NULL OR NEW.workspace_input IS DISTINCT FROM v_expected THEN
     RAISE EXCEPTION 'workspace restore task source is invalid';
   END IF;
   RETURN NEW;
 END IF;
 IF NEW.kind<>'SNAPSHOT' OR NEW.state<>'READY' OR NEW.attempt<>0 OR NEW.generation<>0 OR NEW.archive_id IS NOT NULL OR NEW.maximum_attempts<>5 THEN
   RAISE EXCEPTION 'workspace archive task initial state is invalid';
 END IF;
 v_checkpoint:=NEW.workspace_input->>'checkpoint_ref';
 SELECT jsonb_build_object(
   'target',jsonb_build_object('profile','SESSION_WORKSPACE_V2','scope',jsonb_build_object('organization_ref',o.ref,'project_ref',p.ref,'session_ref',s.ref,'agent_ref',a.ref),
     'workspace_ref',w.ref,'generation',w.generation),
   'checkpoint_ref',v_checkpoint,'quiescence',w.quiescence_record,'pvc_uid',w.writer_acknowledgement::jsonb #>> '{writer,pvc_uid}',
   'reconcile_only',false,'policy',jsonb_build_object('revision',r.safe_snapshot #> '{workspacePolicy,revision}','root',r.safe_snapshot #> '{workspacePolicy,root}',
     'rules',r.safe_snapshot #> '{workspacePolicy,rules}','maximum_writable_bytes',r.safe_snapshot #> '{workspacePolicy,maximumWritableBytes}',
     'maximum_file_count',r.safe_snapshot #> '{workspacePolicy,maximumFileCount}','digest',r.safe_snapshot #> '{workspacePolicy,digest}',
     'denial_reasons',r.safe_snapshot #> '{workspacePolicy,denialReasons}')) INTO v_expected
 FROM control_plane.session_workspace_storage w
 JOIN control_plane.sessions s ON s.id=w.session_id AND s.organization_id=w.organization_id AND s.project_id=w.project_id
 JOIN control_plane.agents a ON a.id=w.agent_id AND a.organization_id=w.organization_id AND a.project_id=w.project_id AND control_plane.workspace_session_scope_matches(s.id,a.id,w.workflow_version_id)
 JOIN control_plane.projects p ON p.id=w.project_id AND p.organization_id=w.organization_id
 JOIN control_plane.organizations o ON o.id=w.organization_id
 JOIN control_plane.runtime_leases l ON l.id=w.writer_lease_id AND l.organization_id=w.organization_id
 JOIN control_plane.runtime_revisions r ON r.id=l.runtime_revision_id AND r.session_id=w.session_id AND r.organization_id=w.organization_id
   AND r.project_id=w.project_id AND r.agent_id=w.agent_id
 WHERE w.id=NEW.workspace_id AND w.organization_id=NEW.organization_id AND w.project_id=NEW.project_id AND w.session_id=NEW.session_id
   AND w.generation=NEW.workspace_generation AND w.state='CHECKPOINT_PENDING' AND w.writer_released
   AND w.quiescence_record IS NOT NULL AND w.current_archive_id IS NULL AND r.safe_snapshot->'roleRuntimeContractRevision'='4'::jsonb
   AND p.lifecycle IN ('ACTIVE','TRASHED')
   AND NEW.object_key='session-archive/v2/'||o.ref||'/'||p.ref||'/'||s.ref||'/'||w.ref||'/g'||w.generation::text||'/'||v_checkpoint||'/manifest.json';
 IF v_expected IS NULL OR NEW.workspace_input IS DISTINCT FROM v_expected OR (v_checkpoint ~ '^wcp_[A-Za-z0-9_-]{8,89}$') IS NOT TRUE THEN
   RAISE EXCEPTION 'workspace archive task source is invalid';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
RESET ROLE;

-- +goose Down
-- Forward-only: существующие workspace сохраняют immutable workflow version.
SELECT 1;
