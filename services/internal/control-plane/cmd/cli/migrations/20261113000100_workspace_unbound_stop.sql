-- +goose Up
SET ROLE control_plane_owner;
SET LOCAL lock_timeout='5s';
LOCK TABLE control_plane.session_workspace_storage,control_plane.session_storage IN ACCESS EXCLUSIVE MODE;

-- +goose StatementBegin
DO $predecessor$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_proc p WHERE p.oid='control_plane.guard_session_workspace_storage()'::regprocedure
  AND p.proowner='control_plane_owner'::regrole AND NOT p.prosecdef AND p.provolatile='v'
  AND p.proconfig=ARRAY['search_path=pg_catalog'] AND md5(p.prosrc)='cea70dfdf75d0fe5c9a943bfc884bcb2') THEN
  RAISE EXCEPTION 'workspace unbound predecessor changed';
 END IF;
END $predecessor$;
-- +goose StatementEnd

CREATE TABLE control_plane.workspace_unbound_stops (
 organization_id uuid NOT NULL REFERENCES control_plane.organizations(id) DEFERRABLE INITIALLY IMMEDIATE,
 workspace_id uuid NOT NULL REFERENCES control_plane.session_workspace_storage(id) DEFERRABLE INITIALLY IMMEDIATE,
 workspace_generation bigint NOT NULL CHECK(workspace_generation>0),
 lease_id uuid PRIMARY KEY REFERENCES control_plane.runtime_leases(id) DEFERRABLE INITIALLY IMMEDIATE,
 previous_state text NOT NULL CHECK(previous_state IN ('EMPTY','RESTORED')),
 record text NOT NULL CHECK(octet_length(record) BETWEEN 1 AND 2048),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
GRANT SELECT,INSERT ON control_plane.workspace_unbound_stops TO control_plane_runtime;
REVOKE UPDATE,DELETE,TRUNCATE ON control_plane.workspace_unbound_stops FROM control_plane_runtime;

-- Обычный runtime не переписывает и не удаляет точные исторические квитанции.
-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_workspace_unbound_stop() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
DECLARE v_record jsonb;
BEGIN
 IF TG_OP='DELETE' AND control_plane.project_purge_authorized() AND EXISTS(
  SELECT 1 FROM control_plane.project_purge_targets target JOIN control_plane.project_purge_context context USING(transaction_id)
  WHERE target.relation_id=TG_RELID AND target.row_key=jsonb_build_array(OLD.lease_id) AND context.transaction_id=pg_current_xact_id_if_assigned()
    AND context.backend_pid=pg_backend_pid()) THEN RETURN OLD; END IF;
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'unbound stop receipt is immutable'; END IF;
 IF NOT (NEW.record IS JSON OBJECT WITH UNIQUE KEYS) THEN RAISE EXCEPTION 'unbound stop receipt scope is invalid'; END IF;
 v_record:=NEW.record::jsonb;
 IF jsonb_typeof(v_record) IS DISTINCT FROM 'object'
  OR v_record->>'schema' IS DISTINCT FROM 'kodex.runtime-workspace-unbound-stop.v1'
  OR (SELECT count(*) FROM jsonb_object_keys(v_record))<>8
  OR NOT(v_record ?& ARRAY['schema','scope','workspace_ref','generation','node_name','node_uid','evidence','execution'])
  OR v_record->>'evidence' NOT IN ('ALL_UNSTARTED','FIRST_INIT_FAILED')
  OR jsonb_typeof(v_record->'evidence') IS DISTINCT FROM 'string'
  OR jsonb_typeof(v_record->'node_name') IS DISTINCT FROM 'string'
  OR length(v_record->>'node_name')>253 OR ((v_record->>'node_name') ~ '^[a-z0-9][a-z0-9.-]*$') IS NOT TRUE
  OR jsonb_typeof(v_record->'node_uid') IS DISTINCT FROM 'string'
  OR ((v_record->>'node_uid') ~ '^[a-z0-9][a-z0-9-]{0,127}$') IS NOT TRUE
  OR jsonb_typeof(v_record->'execution') IS DISTINCT FROM 'object'
  OR (SELECT count(*) FROM jsonb_object_keys(v_record->'execution'))<>14
  OR NOT((v_record->'execution') ?& ARRAY['run_ref','node_ref','turn_ref','runtime_revision_ref','runtime_revision_version',
   'runtime_revision_digest','execution_binding_digest','input_digest','lease_ref','lease_generation','attempt','pod_uid','pvc_uid','join_sha256'])
  OR v_record#>>'{execution,join_sha256}' IS DISTINCT FROM ''
  OR jsonb_typeof(v_record#>'{execution,pod_uid}') IS DISTINCT FROM 'string'
  OR ((v_record#>>'{execution,pod_uid}') ~ '^[a-z0-9][a-z0-9-]{0,127}$') IS NOT TRUE
  OR jsonb_typeof(v_record#>'{execution,pvc_uid}') IS DISTINCT FROM 'string'
  OR ((v_record#>>'{execution,pvc_uid}') ~ '^[a-z0-9][a-z0-9-]{0,127}$') IS NOT TRUE
  OR jsonb_typeof(v_record#>'{execution,execution_binding_digest}') IS DISTINCT FROM 'string'
  OR ((v_record#>>'{execution,execution_binding_digest}') ~ '^[a-f0-9]{64}$') IS NOT TRUE
  OR NOT EXISTS (
   SELECT 1 FROM control_plane.session_workspace_storage workspace
   JOIN control_plane.runtime_leases lease ON lease.id=NEW.lease_id AND lease.organization_id=workspace.organization_id
   JOIN control_plane.runtime_revisions revision ON revision.id=lease.runtime_revision_id
    AND revision.organization_id=workspace.organization_id AND revision.session_id=workspace.session_id
    AND revision.project_id=workspace.project_id AND revision.agent_id=workspace.agent_id
   JOIN control_plane.runs run ON run.id=lease.run_id AND run.organization_id=workspace.organization_id
    AND run.session_id=workspace.session_id AND run.project_id=workspace.project_id
    AND (workspace.workflow_version_id IS NULL OR run.workflow_version_id=workspace.workflow_version_id)
   JOIN control_plane.run_nodes node ON node.id=lease.node_id AND node.organization_id=workspace.organization_id
    AND node.run_id=run.id AND node.agent_id=workspace.agent_id AND node.state IN ('FAILED','CANCELLED','DENIED','SUCCEEDED')
   JOIN control_plane.session_turns turn ON turn.id=revision.turn_id AND turn.organization_id=workspace.organization_id
    AND turn.session_id=workspace.session_id AND turn.run_id=run.id AND node.turn_id=turn.id AND node.attempt=revision.attempt
   JOIN control_plane.sessions session ON session.id=workspace.session_id AND session.organization_id=workspace.organization_id
   JOIN control_plane.agents agent ON agent.id=workspace.agent_id AND agent.organization_id=workspace.organization_id
   JOIN control_plane.projects project ON project.id=workspace.project_id AND project.organization_id=workspace.organization_id
   JOIN control_plane.organizations organization ON organization.id=workspace.organization_id
   JOIN control_plane.session_storage native ON native.session_id=workspace.session_id AND native.organization_id=workspace.organization_id
    AND native.native_writer_lease_ref=lease.ref AND native.native_writer_revision_id=revision.id
   WHERE workspace.id=NEW.workspace_id AND workspace.organization_id=NEW.organization_id
    AND workspace.generation=NEW.workspace_generation AND workspace.state='WRITER_PENDING'
    AND workspace.writer_lease_id=lease.id AND workspace.writer_acknowledgement IS NULL AND workspace.quiescence_record IS NULL
    AND workspace.block_reason='' AND NOT workspace.writer_released AND workspace.destructive_task_id IS NULL
    AND lease.state IN ('COMPLETED','CANCELLED','EXPIRED')
    AND ((native.state='INITIALIZING' AND native.native_session_id IS NULL AND native.native_checkpoint IS NULL
      AND native.content_generation=0 AND native.current_archive_id IS NULL)
     OR (native.state='LIVE' AND native.native_checkpoint IS NOT NULL AND native.content_generation>0))
    AND NOT EXISTS(SELECT 1 FROM control_plane.integration_invocations effect
     WHERE effect.organization_id=workspace.organization_id AND effect.node_id=node.id AND effect.risk<>'READ')
    AND NOT EXISTS(SELECT 1 FROM control_plane.repository_bundle_publications effect
     WHERE effect.organization_id=workspace.organization_id AND effect.origin_node_id=node.id)
    AND v_record->>'workspace_ref'=workspace.ref AND v_record->'generation'=to_jsonb(workspace.generation)
    AND v_record->'scope'=jsonb_build_object('organization_ref',organization.ref,'project_ref',project.ref,'session_ref',session.ref,'agent_ref',agent.ref)
    AND v_record#>'{execution,lease_ref}'=to_jsonb(lease.ref)
    AND v_record#>'{execution,runtime_revision_ref}'=to_jsonb(revision.ref)
    AND v_record#>'{execution,runtime_revision_digest}'=to_jsonb(revision.revision_digest)
    AND v_record#>'{execution,runtime_revision_version}'=to_jsonb(revision.generation)
    AND v_record#>'{execution,input_digest}'=to_jsonb(lease.input_digest)
    AND v_record#>'{execution,lease_generation}'=to_jsonb(lease.generation)
    AND v_record#>'{execution,run_ref}'=to_jsonb(run.ref)
    AND v_record#>'{execution,node_ref}'=to_jsonb(node.ref)
    AND v_record#>'{execution,turn_ref}'=to_jsonb(turn.ref)
    AND v_record#>'{execution,attempt}'=to_jsonb(revision.attempt)
    AND v_record#>'{execution,pvc_uid}'=revision.safe_snapshot#>'{workspaceStorage,volume_ready,preparation,pvc_uid}'
    AND NEW.previous_state=CASE WHEN workspace.source_archive_id IS NULL THEN 'EMPTY' ELSE 'RESTORED' END
  ) THEN RAISE EXCEPTION 'unbound stop receipt scope is invalid'; END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER guard_workspace_unbound_stop BEFORE INSERT OR UPDATE OR DELETE ON control_plane.workspace_unbound_stops
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_workspace_unbound_stop();
REVOKE ALL ON FUNCTION control_plane.guard_workspace_unbound_stop() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.guard_workspace_unbound_stop() TO control_plane_runtime;

-- Сохраняем predecessor guard; снять intent позволяет лишь committed exact no-BIND fact.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.guard_session_workspace_storage() RETURNS trigger
LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog AS $$
DECLARE
    v_ack jsonb;
    v_join jsonb;
    v_unbound boolean := false;
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
        v_unbound := OLD.state='WRITER_PENDING' AND NEW.state IN ('EMPTY','RESTORED')
            AND NEW.generation=OLD.generation AND OLD.writer_acknowledgement IS NULL AND NEW.writer_acknowledgement IS NULL
            AND OLD.quiescence_record IS NULL AND NEW.quiescence_record IS NULL AND NOT OLD.writer_released AND NOT NEW.writer_released
            AND OLD.block_reason='' AND NEW.block_reason='' AND NEW.writer_lease_id IS NULL
            AND NEW.current_archive_id IS NOT DISTINCT FROM OLD.current_archive_id
            AND NEW.source_archive_id IS NOT DISTINCT FROM OLD.source_archive_id
            AND NEW.destructive_task_id IS NOT DISTINCT FROM OLD.destructive_task_id
            AND EXISTS (SELECT 1 FROM control_plane.workspace_unbound_stops proof
                WHERE proof.organization_id=OLD.organization_id AND proof.workspace_id=OLD.id
                    AND proof.workspace_generation=OLD.generation AND proof.lease_id=OLD.writer_lease_id
                    AND proof.previous_state=NEW.state);
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
            (OLD.state='WRITER_PENDING' AND NEW.state='WRITING') OR v_unbound OR
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
            (OLD.writer_lease_id IS NOT NULL AND NEW.writer_lease_id IS DISTINCT FROM OLD.writer_lease_id AND NOT v_unbound) OR
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

-- Extend the exact retained project graph only by this table and its three fixed foreign keys.
-- +goose StatementBegin
DO $unbound_purge$
DECLARE
 v_function oid:='control_plane.purge_project_database(uuid,uuid,text)'::regprocedure;
 v_definition text;v_source text;v_nodes oid[];v_count integer;v_digest text;
 v_old_nodes text:='IF v_nodes <> 111 OR v_node_fingerprint <> ''3eb558fae167872ba6bd17bc68f7a303'' THEN';
 v_old_edges text:='IF v_edges <> 308 OR v_edge_fingerprint <> ''d6aa5b9aa21c5e065c3106f2a6119e0f'' THEN';
 v_new_nodes text;v_new_edges text;
BEGIN
 SELECT pg_get_functiondef(p.oid),p.prosrc INTO STRICT v_definition,v_source FROM pg_proc p
 WHERE p.oid=v_function AND p.proowner='control_plane_owner'::regrole AND p.prosecdef
 AND p.proconfig=ARRAY['search_path=pg_catalog'] AND p.provolatile='v'
 AND has_function_privilege('control_plane_runtime',p.oid,'EXECUTE');
 IF regexp_count(v_source,'IF v_nodes <>')<>1 OR regexp_count(v_source,'IF v_edges <>')<>1
 OR md5(replace(replace(v_source,v_old_nodes,
 'IF v_nodes <> 105 OR v_node_fingerprint <> ''f51a0de25bd942a250997775617eeaee'' THEN'),v_old_edges,
 'IF v_edges <> 284 OR v_edge_fingerprint <> ''6bf6ad76d8c7ae66e07f75ecf719fdd6'' THEN'))<>'b886c24d4e2f5c4a0ec08123d99462b0'
 THEN RAISE EXCEPTION 'unbound stop purge predecessor changed';END IF;
 IF (SELECT count(*) FROM pg_constraint WHERE conrelid='control_plane.workspace_unbound_stops'::regclass AND contype='f')<>3
 THEN RAISE EXCEPTION 'unbound stop foreign key delta changed';END IF;
 WITH RECURSIVE nodes(id) AS (SELECT 'control_plane.projects'::regclass::oid UNION
 SELECT c.conrelid FROM pg_constraint c JOIN nodes n ON c.confrelid=n.id
 WHERE c.contype='f' AND c.connamespace='control_plane'::regnamespace)
 SELECT array_agg(id) INTO v_nodes FROM nodes;
 SELECT count(*),md5(string_agg(n.nspname||'.'||r.relname,E'\n' ORDER BY n.nspname,r.relname)) INTO v_count,v_digest
 FROM pg_class r JOIN pg_namespace n ON n.oid=r.relnamespace
 WHERE r.oid=ANY(v_nodes) AND r.oid<>'control_plane.workspace_unbound_stops'::regclass;
 IF v_count<>111 OR v_digest<>'3eb558fae167872ba6bd17bc68f7a303' THEN RAISE EXCEPTION 'unbound stop predecessor nodes changed';END IF;
 SELECT count(*),md5(string_agg(cn.nspname||'.'||cc.relname||'|'||c.conname||'|'||pn.nspname||'.'||pc.relname||'|'||c.confdeltype::text,
 E'\n' ORDER BY cn.nspname,cc.relname,c.conname)) INTO v_count,v_digest
 FROM pg_constraint c JOIN pg_class cc ON cc.oid=c.conrelid JOIN pg_namespace cn ON cn.oid=cc.relnamespace
 JOIN pg_class pc ON pc.oid=c.confrelid JOIN pg_namespace pn ON pn.oid=pc.relnamespace
 WHERE c.contype='f' AND c.conrelid=ANY(v_nodes) AND c.confrelid=ANY(v_nodes)
 AND c.conrelid<>'control_plane.workspace_unbound_stops'::regclass;
 IF v_count<>308 OR v_digest<>'d6aa5b9aa21c5e065c3106f2a6119e0f' THEN RAISE EXCEPTION 'unbound stop predecessor edges changed';END IF;
 SELECT count(*),md5(string_agg(n.nspname||'.'||r.relname,E'\n' ORDER BY n.nspname,r.relname)) INTO v_count,v_digest
 FROM pg_class r JOIN pg_namespace n ON n.oid=r.relnamespace WHERE r.oid=ANY(v_nodes);
 IF v_count<>112 THEN RAISE EXCEPTION 'unbound stop successor nodes changed';END IF;
 v_new_nodes:=format('IF v_nodes <> %s OR v_node_fingerprint <> %L THEN',v_count,v_digest);
 SELECT count(*),md5(string_agg(cn.nspname||'.'||cc.relname||'|'||c.conname||'|'||pn.nspname||'.'||pc.relname||'|'||c.confdeltype::text,
 E'\n' ORDER BY cn.nspname,cc.relname,c.conname)) INTO v_count,v_digest
 FROM pg_constraint c JOIN pg_class cc ON cc.oid=c.conrelid JOIN pg_namespace cn ON cn.oid=cc.relnamespace
 JOIN pg_class pc ON pc.oid=c.confrelid JOIN pg_namespace pn ON pn.oid=pc.relnamespace
 WHERE c.contype='f' AND c.conrelid=ANY(v_nodes) AND c.confrelid=ANY(v_nodes);
 IF v_count<>310 THEN RAISE EXCEPTION 'unbound stop successor edges changed';END IF;
 v_new_edges:=format('IF v_edges <> %s OR v_edge_fingerprint <> %L THEN',v_count,v_digest);
 EXECUTE replace(replace(v_definition,v_old_nodes,v_new_nodes),v_old_edges,v_new_edges);
 IF NOT EXISTS(SELECT 1 FROM pg_proc p WHERE p.oid=v_function AND p.prosrc=replace(replace(v_source,v_old_nodes,v_new_nodes),v_old_edges,v_new_edges))
 THEN RAISE EXCEPTION 'unbound stop purge readback failed';END IF;
END $unbound_purge$;
-- +goose StatementEnd
RESET ROLE;
